//go:build linux && cgo && (production || dev)

package main

/*
#cgo pkg-config: gtk+-3.0

#include <gtk/gtk.h>

// A clipboard read handed to the GTK main thread; the caller waits on cond until done.
typedef struct {
	GMutex mu;
	GCond cond;
	gboolean done;
	gchar *png;
	gsize len;
} clipRead;

static gboolean readClipboardImage(gpointer p) {
	clipRead *r = p;
	gchar *png = NULL;
	gsize len = 0;
	GdkPixbuf *img = gtk_clipboard_wait_for_image(gtk_clipboard_get(GDK_SELECTION_CLIPBOARD));
	if (img) {
		if (!gdk_pixbuf_save_to_buffer(img, &png, &len, "png", NULL, NULL)) png = NULL, len = 0;
		g_object_unref(img);
	}
	g_mutex_lock(&r->mu);
	r->png = png;
	r->len = len;
	r->done = TRUE;
	g_cond_signal(&r->cond);
	g_mutex_unlock(&r->mu);
	return G_SOURCE_REMOVE;
}

// Must not be called on the main thread (it waits for it).
static gchar *clipboardPNG(gsize *len) {
	clipRead r = {0};
	g_mutex_init(&r.mu);
	g_cond_init(&r.cond);
	g_idle_add(readClipboardImage, &r);
	g_mutex_lock(&r.mu);
	while (!r.done) g_cond_wait(&r.cond, &r.mu);
	g_mutex_unlock(&r.mu);
	g_mutex_clear(&r.mu);
	g_cond_clear(&r.cond);
	*len = r.len;
	return r.png;
}
*/
import "C"

import (
	"encoding/base64"
	"unsafe"
)

// ClipboardImage returns the clipboard's image as base64 PNG, or "" if it holds none.
// It is the composer's paste fallback: WebKitGTK (on Wayland at least) leaves
// screenshots out of the paste event, so GTK reads the clipboard on its main thread.
// Bound methods run off the main thread, so the wait cannot deadlock.
func (a *App) ClipboardImage() string {
	var n C.gsize
	png := C.clipboardPNG(&n)
	if png == nil {
		return ""
	}
	defer C.g_free(C.gpointer(png))
	return base64.StdEncoding.EncodeToString(C.GoBytes(unsafe.Pointer(png), C.int(n)))
}
