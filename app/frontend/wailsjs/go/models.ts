export namespace main {
	
	export class Attachment {
	    name: string;
	    media_type: string;
	    data: string;
	
	    static createFrom(source: any = {}) {
	        return new Attachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.media_type = source["media_type"];
	        this.data = source["data"];
	    }
	}
	export class AttachmentInfo {
	    name: string;
	    media_type: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new AttachmentInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.media_type = source["media_type"];
	        this.size = source["size"];
	    }
	}
	export class ChangedFile {
	    path: string;
	    old_path: string;
	    status: string;
	    added: number;
	    removed: number;
	    binary: boolean;
	    sig: string;

	    static createFrom(source: any = {}) {
	        return new ChangedFile(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.old_path = source["old_path"];
	        this.status = source["status"];
	        this.added = source["added"];
	        this.removed = source["removed"];
	        this.binary = source["binary"];
	        this.sig = source["sig"];
	    }
	}
	export class Changes {
	    is_repo: boolean;
	    files: ChangedFile[];

	    static createFrom(source: any = {}) {
	        return new Changes(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.is_repo = source["is_repo"];
	        this.files = this.convertValues(source["files"], ChangedFile);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChatItem {
	    id: number;
	    kind: string;
	    text?: string;
	    name?: string;
	    summary?: string;
	    escalation?: notes.Escalation;
	    attachments?: AttachmentInfo[];
	    at: string;
	
	    static createFrom(source: any = {}) {
	        return new ChatItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.text = source["text"];
	        this.name = source["name"];
	        this.summary = source["summary"];
	        this.escalation = this.convertValues(source["escalation"], notes.Escalation);
	        this.attachments = this.convertValues(source["attachments"], AttachmentInfo);
	        this.at = source["at"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DiffLine {
	    kind: string;
	    text: string;
	    old_no: number;
	    new_no: number;

	    static createFrom(source: any = {}) {
	        return new DiffLine(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.text = source["text"];
	        this.old_no = source["old_no"];
	        this.new_no = source["new_no"];
	    }
	}
	export class Hunk {
	    old_start: number;
	    old_lines: number;
	    new_start: number;
	    new_lines: number;
	    lines: DiffLine[];

	    static createFrom(source: any = {}) {
	        return new Hunk(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.old_start = source["old_start"];
	        this.old_lines = source["old_lines"];
	        this.new_start = source["new_start"];
	        this.new_lines = source["new_lines"];
	        this.lines = this.convertValues(source["lines"], DiffLine);
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class FileDiff {
	    path: string;
	    binary: boolean;
	    too_large: boolean;
	    hunks: Hunk[];
	    file_lines: string[];

	    static createFrom(source: any = {}) {
	        return new FileDiff(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.binary = source["binary"];
	        this.too_large = source["too_large"];
	        this.hunks = this.convertValues(source["hunks"], Hunk);
	        this.file_lines = source["file_lines"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LimitWindow {
	    utilization: number;
	    resets_at: number;
	
	    static createFrom(source: any = {}) {
	        return new LimitWindow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.utilization = source["utilization"];
	        this.resets_at = source["resets_at"];
	    }
	}
	export class RateLimit {
	    five_hour?: LimitWindow;
	    seven_day?: LimitWindow;
	
	    static createFrom(source: any = {}) {
	        return new RateLimit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.five_hour = this.convertValues(source["five_hour"], LimitWindow);
	        this.seven_day = this.convertValues(source["seven_day"], LimitWindow);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SessionSnapshot {
	    session: notes.Session;
	    agents: notes.Agent[];
	    tasks: notes.Task[];
	    notes: notes.Note[];
	    chat: ChatItem[];
	    escalations: notes.Escalation[];
	
	    static createFrom(source: any = {}) {
	        return new SessionSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session = this.convertValues(source["session"], notes.Session);
	        this.agents = this.convertValues(source["agents"], notes.Agent);
	        this.tasks = this.convertValues(source["tasks"], notes.Task);
	        this.notes = this.convertValues(source["notes"], notes.Note);
	        this.chat = this.convertValues(source["chat"], ChatItem);
	        this.escalations = this.convertValues(source["escalations"], notes.Escalation);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Settings {
	    auto_compact_tokens: number;

	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.auto_compact_tokens = source["auto_compact_tokens"];
	    }
	}

}

export namespace notes {
	
	export class Agent {
	    id: number;
	    session_id: number;
	    parent_id?: number;
	    role: string;
	    task_id?: number;
	    status: string;
	    pid?: number;
	    log_path?: string;
	    exit_code?: number;
	    created_at: string;
	    exited_at?: string;
	    context_used?: number;
	    context_window?: number;
	    model?: string;
	
	    static createFrom(source: any = {}) {
	        return new Agent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.parent_id = source["parent_id"];
	        this.role = source["role"];
	        this.task_id = source["task_id"];
	        this.status = source["status"];
	        this.pid = source["pid"];
	        this.log_path = source["log_path"];
	        this.exit_code = source["exit_code"];
	        this.created_at = source["created_at"];
	        this.exited_at = source["exited_at"];
	        this.context_used = source["context_used"];
	        this.context_window = source["context_window"];
	        this.model = source["model"];
	    }
	}
	export class AgentEvent {
	    id: number;
	    agent_id: number;
	    event_type: string;
	    payload: string;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentEvent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.agent_id = source["agent_id"];
	        this.event_type = source["event_type"];
	        this.payload = source["payload"];
	        this.created_at = source["created_at"];
	    }
	}
	export class Escalation {
	    id: number;
	    session_id: number;
	    agent_id: number;
	    question: string;
	    context: string;
	    status: string;
	    answer?: string;
	
	    static createFrom(source: any = {}) {
	        return new Escalation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.agent_id = source["agent_id"];
	        this.question = source["question"];
	        this.context = source["context"];
	        this.status = source["status"];
	        this.answer = source["answer"];
	    }
	}
	export class Note {
	    id: number;
	    board_id: number;
	    author_agent_id: number;
	    type: string;
	    content: string;
	    status: string;
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new Note(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.board_id = source["board_id"];
	        this.author_agent_id = source["author_agent_id"];
	        this.type = source["type"];
	        this.content = source["content"];
	        this.status = source["status"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class Session {
	    provider: string;
	    id: number;
	    title: string;
	    status: string;
	    work_dir: string;
	    created_at: string;
	    agent_count: number;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.work_dir = source["work_dir"];
	        this.provider = source["provider"];
	        this.created_at = source["created_at"];
	        this.agent_count = source["agent_count"];
	    }
	}
	export class Task {
	    id: number;
	    session_id: number;
	    title: string;
	    description: string;
	    status: string;
	    agent_id?: number;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.session_id = source["session_id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.status = source["status"];
	        this.agent_id = source["agent_id"];
	    }
	}

}

