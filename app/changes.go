package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// The Code changes panel: a session's uncommitted changes (tracked and
// untracked) and one file's parsed diff. Everything runs read-only git in the
// session's work dir; the index is never touched.
const (
	emptyTree     = "4b825dc642cb6eb9a060e54bf8d69288fbee4904" // HEAD stand-in in a repo with no commits
	maxDiffBytes  = 1 << 20
	maxDiffLines  = 20000
	binarySniffer = 8000 // bytes checked for a NUL
)

type Changes struct {
	IsRepo bool          `json:"is_repo"`
	Files  []ChangedFile `json:"files"`
}

type ChangedFile struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path"` // "" unless renamed
	Status  string `json:"status"`   // M, A, D, R, or ? for untracked
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Binary  bool   `json:"binary"`
	Sig     string `json:"sig"` // changes whenever the file's diff could change
}

type FileDiff struct {
	Path      string   `json:"path"`
	Binary    bool     `json:"binary"`
	TooLarge  bool     `json:"too_large"`
	Hunks     []Hunk   `json:"hunks"`
	FileLines []string `json:"file_lines"` // working-tree file, empty for deleted files
}

type Hunk struct {
	OldStart int        `json:"old_start"`
	OldLines int        `json:"old_lines"`
	NewStart int        `json:"new_start"`
	NewLines int        `json:"new_lines"`
	Lines    []DiffLine `json:"lines"`
}

// DiffLine's Kind is " ", "+" or "-"; OldNo/NewNo are 0 on the side the line is not on.
type DiffLine struct {
	Kind  string `json:"kind"`
	Text  string `json:"text"`
	OldNo int    `json:"old_no"`
	NewNo int    `json:"new_no"`
}

// ListRepos returns the git repos the panel can show, as paths relative to the
// work dir: [""] when the work dir is itself in a repo, else its immediate
// subdirectories that are repos, sorted. Empty means no repo.
func (a *App) ListRepos(sessionID int64) ([]string, error) {
	root, err := a.changesDir(sessionID)
	if err != nil {
		return nil, err
	}
	return listRepos(root), nil
}

func listRepos(root string) []string {
	if isGitRepo(root) {
		return []string{""}
	}
	repos := []string{}
	// ponytail: depth 1 is the ceiling (no recursive walk); raise it if repos nest deeper than work_dir/<repo>
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		n := e.Name()
		if !e.IsDir() || strings.HasPrefix(n, ".") || n == "node_modules" { // symlinks are not IsDir, so none escape
			continue
		}
		if _, err := os.Stat(filepath.Join(root, n, ".git")); err == nil {
			repos = append(repos, n)
		}
	}
	sort.Strings(repos)
	return repos
}

// repoDir resolves repo (as listed by ListRepos; "" is the work dir) to the
// directory git runs in.
func (a *App) repoDir(sessionID int64, repo string) (string, error) {
	root, err := a.changesDir(sessionID)
	if err != nil {
		return "", err
	}
	if repo == "" {
		return root, nil
	}
	rel, err := cleanRelPath(repo)
	if err != nil {
		return "", err
	}
	for _, r := range listRepos(root) {
		if r == rel {
			return filepath.Join(root, filepath.FromSlash(rel)), nil
		}
	}
	return "", fmt.Errorf("unknown repo %q", repo)
}

// GetChanges lists the selected repo's uncommitted files, sorted by path. A
// repo that is not a git repo gives is_repo=false and no error.
func (a *App) GetChanges(sessionID int64, repo string) (Changes, error) {
	dir, err := a.repoDir(sessionID, repo)
	if err != nil {
		return Changes{}, err
	}
	res := Changes{Files: []ChangedFile{}}
	if !isGitRepo(dir) {
		return res, nil
	}
	res.IsRepo = true
	if res.Files, err = listChanges(dir); err != nil {
		return Changes{}, err
	}
	return res, nil
}

// GetFileDiff returns one file's diff against HEAD (-U3 hunks) and its
// working-tree lines. path is relative to the repo dir.
func (a *App) GetFileDiff(sessionID int64, repo, path string) (FileDiff, error) {
	dir, err := a.repoDir(sessionID, repo)
	if err != nil {
		return FileDiff{}, err
	}
	rel, err := cleanRelPath(path)
	if err != nil {
		return FileDiff{}, err
	}
	if !isGitRepo(dir) {
		return FileDiff{}, errors.New("not a git repository")
	}
	files, err := listChanges(dir)
	if err != nil {
		return FileDiff{}, err
	}
	var cf *ChangedFile
	for i := range files {
		if files[i].Path == rel {
			cf = &files[i]
		}
	}
	d := FileDiff{Path: rel, Hunks: []Hunk{}, FileLines: []string{}}
	if cf != nil && cf.Binary {
		d.Binary = true
		return d, nil
	}

	var content []byte
	if cf == nil || cf.Status != "D" {
		var tooBig bool
		if content, tooBig, err = readWorkingFile(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			return FileDiff{}, err
		}
		if tooBig {
			d.TooLarge = true
			return d, nil
		}
		if isBinary(content) {
			d.Binary = true
			return d, nil
		}
	}
	lines := splitLines(content)

	switch {
	case cf == nil:
	case cf.Status == "?":
		h := Hunk{NewStart: 1, NewLines: len(lines), Lines: make([]DiffLine, len(lines))}
		for i, l := range lines {
			h.Lines[i] = DiffLine{Kind: "+", Text: l, NewNo: i + 1}
		}
		if len(lines) > 0 {
			d.Hunks = append(d.Hunks, h)
		}
	default:
		args := []string{"diff", diffBase(dir), "-M", "-U3", "--relative", "--", rel}
		if cf.OldPath != "" {
			args = append(args, cf.OldPath)
		}
		out, err := git(dir, args...)
		if err != nil {
			return FileDiff{}, err
		}
		if len(out) > maxDiffBytes {
			d.TooLarge = true
			return d, nil
		}
		d.Hunks = parseHunks(string(out))
	}

	n := 0
	for _, h := range d.Hunks {
		n += len(h.Lines)
	}
	if n > maxDiffLines {
		d.TooLarge = true
		d.Hunks = []Hunk{}
		return d, nil
	}
	d.FileLines = lines
	return d, nil
}

func (a *App) changesDir(sessionID int64) (string, error) {
	se, err := a.store.GetSession(sessionID)
	if err != nil {
		return "", err
	}
	if se.WorkDir == "" {
		return "", errors.New("session has no work dir")
	}
	return se.WorkDir, nil
}

// cleanRelPath rejects absolute paths and paths that leave their base dir, and
// returns the cleaned slash-separated path.
func cleanRelPath(p string) (string, error) {
	c := filepath.Clean(p)
	if p == "" || filepath.IsAbs(p) || c == "." || c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid path %q", p)
	}
	return filepath.ToSlash(c), nil
}

func git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-c", "core.quotepath=off"}, args...)...)
	cmd.Dir = dir
	// optional locks would let git refresh (write) the index; paths are never pathspec magic
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_LITERAL_PATHSPECS=1")
	return cmd.Output()
}

func isGitRepo(dir string) bool {
	out, err := git(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// diffBase is HEAD, or the empty tree in a repo with no commits.
func diffBase(dir string) string {
	if _, err := git(dir, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		return emptyTree
	}
	return "HEAD"
}

func listChanges(dir string) ([]ChangedFile, error) {
	out, err := git(dir, "diff", diffBase(dir), "-M", "--relative", "--raw", "--numstat", "-z")
	if err != nil {
		return nil, err
	}
	byPath := map[string]*ChangedFile{}
	toks := strings.Split(string(out), "\x00")
loop:
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case strings.HasPrefix(t, ":"): // :<modes> <shas> <status>, then the path(s)
			f := strings.Fields(t)
			status := f[len(f)-1][:1]
			cf := &ChangedFile{Status: status}
			switch status {
			case "R", "C":
				if i+2 >= len(toks) {
					break loop
				}
				cf.OldPath, cf.Path = toks[i+1], toks[i+2]
				cf.Status = "R"
				i += 2
			default:
				if i+1 >= len(toks) {
					break loop
				}
				cf.Path = toks[i+1]
				if status != "A" && status != "D" {
					cf.Status = "M" // also covers type changes and unmerged
				}
				i++
			}
			byPath[cf.Path] = cf
		case strings.Count(t, "\t") >= 2: // added<TAB>removed<TAB>path; renames have an empty path and two more tokens
			p := strings.SplitN(t, "\t", 3)
			path := p[2]
			if path == "" {
				if i+2 >= len(toks) {
					break loop
				}
				path = toks[i+2]
				i += 2
			}
			if cf := byPath[path]; cf != nil {
				if p[0] == "-" {
					cf.Binary = true
				} else {
					cf.Added, _ = strconv.Atoi(p[0])
					cf.Removed, _ = strconv.Atoi(p[1])
				}
			}
		}
	}

	files := make([]ChangedFile, 0, len(byPath))
	for _, cf := range byPath {
		files = append(files, *cf)
	}

	out, err = git(dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" {
			continue
		}
		cf := ChangedFile{Path: p, Status: "?"}
		cf.Added, cf.Binary = countUntracked(filepath.Join(dir, p))
		files = append(files, cf)
	}

	for i := range files {
		f := &files[i]
		var mtime, size int64
		if fi, err := os.Lstat(filepath.Join(dir, f.Path)); err == nil {
			mtime, size = fi.ModTime().UnixNano(), fi.Size()
		}
		f.Sig = fmt.Sprintf("%s|%s|%d|%d|%t|%d|%d", f.Status, f.OldPath, f.Added, f.Removed, f.Binary, mtime, size)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// countUntracked returns an untracked file's line count, or binary=true when
// its first bytes hold a NUL. Anything that is not a regular file counts 0.
func countUntracked(path string) (lines int, binary bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return 0, false
	}
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	buf := make([]byte, 64<<10)
	var last byte
	for first := true; ; first = false {
		n, err := f.Read(buf)
		if n > 0 {
			if first && isBinary(buf[:n]) {
				return 0, true
			}
			lines += bytes.Count(buf[:n], []byte{'\n'})
			last = buf[n-1]
		}
		if err != nil {
			break
		}
	}
	if last != 0 && last != '\n' {
		lines++
	}
	return lines, false
}

func isBinary(b []byte) bool {
	if len(b) > binarySniffer {
		b = b[:binarySniffer]
	}
	return bytes.IndexByte(b, 0) >= 0
}

// readWorkingFile reads a regular file of at most maxDiffBytes; bigger ones
// come back with tooBig=true and no content.
func readWorkingFile(path string) (content []byte, tooBig bool, err error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, false, err
	}
	if !fi.Mode().IsRegular() {
		return nil, false, nil // symlinks are not followed: they could point outside the work dir
	}
	if fi.Size() > maxDiffBytes {
		return nil, true, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	content, err = io.ReadAll(io.LimitReader(f, maxDiffBytes+1))
	if len(content) > maxDiffBytes {
		return nil, true, nil
	}
	return content, false, err
}

// splitLines splits on "\n" without an empty element for a final newline.
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// parseHunks reads the @@ hunks of a single-file unified diff.
func parseHunks(diff string) []Hunk {
	hunks := []Hunk{}
	lines := strings.Split(diff, "\n")
	for i := 0; i < len(lines); i++ {
		h, ok := parseHunkHeader(lines[i])
		if !ok {
			continue
		}
		old, new := h.OldStart, h.NewStart
		ro, rn := h.OldLines, h.NewLines
		h.Lines = []DiffLine{}
	body:
		for ro > 0 || rn > 0 {
			if i+1 >= len(lines) || lines[i+1] == "" {
				break
			}
			i++
			l := lines[i]
			switch l[0] {
			case ' ':
				h.Lines = append(h.Lines, DiffLine{Kind: " ", Text: l[1:], OldNo: old, NewNo: new})
				old, new, ro, rn = old+1, new+1, ro-1, rn-1
			case '-':
				h.Lines = append(h.Lines, DiffLine{Kind: "-", Text: l[1:], OldNo: old})
				old, ro = old+1, ro-1
			case '+':
				h.Lines = append(h.Lines, DiffLine{Kind: "+", Text: l[1:], NewNo: new})
				new, rn = new+1, rn-1
			case '\\': // "\ No newline at end of file"
			default:
				break body
			}
		}
		hunks = append(hunks, h)
	}
	return hunks
}

// parseHunkHeader reads "@@ -a[,b] +c[,d] @@ ...".
func parseHunkHeader(l string) (Hunk, bool) {
	f := strings.Fields(l)
	if len(f) < 3 || f[0] != "@@" || !strings.HasPrefix(f[1], "-") || !strings.HasPrefix(f[2], "+") {
		return Hunk{}, false
	}
	var h Hunk
	var ok1, ok2 bool
	h.OldStart, h.OldLines, ok1 = parseRange(f[1][1:])
	h.NewStart, h.NewLines, ok2 = parseRange(f[2][1:])
	return h, ok1 && ok2
}

// parseRange reads "start" or "start,count"; a missing count is 1.
func parseRange(s string) (start, n int, ok bool) {
	a, b, hasCount := strings.Cut(s, ",")
	start, err := strconv.Atoi(a)
	if err != nil {
		return 0, 0, false
	}
	n = 1
	if hasCount {
		if n, err = strconv.Atoi(b); err != nil {
			return 0, 0, false
		}
	}
	return start, n, true
}
