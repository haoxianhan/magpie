#include <gtk/gtk.h>
#include <math.h>

#ifdef MAGPIE_DPI_GUARD
#include "webkit_dpi_gtk3.h"
#endif
#include <webkit2/webkit2.h>
#include <stdio.h>
#include <stdlib.h>

static GMainLoop *loop;
static int remaining = 2;
static int failed;
static int initial_device;
static gboolean timeout_fired;

static void measured(GObject *view, GAsyncResult *result, gpointer data) {
	GError *error = NULL;
	JSCValue *value = webkit_web_view_evaluate_javascript_finish(WEBKIT_WEB_VIEW(view), result, &error);
	if (!value) {
		fprintf(stderr, "JavaScript: %s\n", error ? error->message : "no result");
		g_clear_error(&error);
		failed = 1;
	} else {
		char *metrics = jsc_value_to_string(value);
		printf("layout %d %.8g %s %d %d %d\n", GPOINTER_TO_INT(data),
			gdk_screen_get_resolution(gdk_screen_get_default()), metrics,
			gtk_widget_get_allocated_width(GTK_WIDGET(view)), gtk_widget_get_scale_factor(GTK_WIDGET(view)), initial_device);
		g_free(metrics);
		g_object_unref(value);
	}
	if (--remaining == 0) g_main_loop_quit(loop);
}

static void loaded(WebKitWebView *view, WebKitLoadEvent event, gpointer data) {
	if (event == WEBKIT_LOAD_FINISHED)
		webkit_web_view_evaluate_javascript(view,
			"[innerWidth,innerHeight,document.querySelector('main').getBoundingClientRect().width,parseFloat(getComputedStyle(document.body).fontSize)].join(' ')",
			-1, NULL, NULL, NULL, measured, data);
}

static gboolean timed_out(gpointer data) {
	(void)data;
	timeout_fired = TRUE;
	fprintf(stderr, "Timed out waiting for native WebKit layout\n");
	failed = 1;
	g_main_loop_quit(loop);
	return G_SOURCE_REMOVE;
}

int main(int argc, char **argv) {
	if (argc != 3) {
		fprintf(stderr, "Usage: %s DPI ZOOM\n", argv[0]);
		return 2;
	}
	char *end;
	double dpi = g_ascii_strtod(argv[1], &end);
	if (end == argv[1] || *end || !isfinite(dpi)) return 2;
	double zoom = g_ascii_strtod(argv[2], &end);
	if (end == argv[2] || *end || !isfinite(zoom) || zoom <= 0) return 2;
	if (!gtk_init_check(NULL, NULL)) {
		fprintf(stderr, "GTK initialization failed: no usable display\n");
		return 3;
	}
	gint configured = -1;
	g_object_get(gtk_settings_get_default(), "gtk-xft-dpi", &configured, NULL);
	printf("GTK %u.%u.%u WebKitGTK %u.%u.%u initialDPI %.8g configuredDPI %d requestedDPI %.8g\n",
		gtk_get_major_version(), gtk_get_minor_version(), gtk_get_micro_version(),
		webkit_get_major_version(), webkit_get_minor_version(), webkit_get_micro_version(),
		gdk_screen_get_resolution(gdk_screen_get_default()), configured, dpi);
	initial_device = gdk_screen_get_monitor_scale_factor(gdk_screen_get_default(), 0);
	// Even a positive settings.ini property can leave GDK unset. Reproduce
	// that state explicitly; application-owned GtkSettings would mask it.
	gdk_screen_set_resolution(gdk_screen_get_default(), dpi);
#ifdef MAGPIE_DPI_GUARD
	if (!prepare_native_webkit()) return 3;
#endif
	loop = g_main_loop_new(NULL, FALSE);
	GtkWidget *windows[2];
	for (int i = 0; i < 2; i++) {
		windows[i] = gtk_window_new(GTK_WINDOW_TOPLEVEL);
		gtk_window_set_title(GTK_WINDOW(windows[i]), "Magpie DPI regression probe");
		gtk_window_set_accept_focus(GTK_WINDOW(windows[i]), FALSE);
		gtk_window_set_default_size(GTK_WINDOW(windows[i]), i == 0 ? 948 : 420, 600);
		if (i == 1) {
			gtk_window_set_transient_for(GTK_WINDOW(windows[i]), GTK_WINDOW(windows[0]));
			gtk_window_set_resizable(GTK_WINDOW(windows[i]), FALSE);
		}
		GtkWidget *view = webkit_web_view_new();
		webkit_web_view_set_zoom_level(WEBKIT_WEB_VIEW(view), zoom);
		gtk_container_add(GTK_CONTAINER(windows[i]), view);
		g_signal_connect(view, "load-changed", G_CALLBACK(loaded), GINT_TO_POINTER(i));
		gtk_widget_show_all(windows[i]);
		webkit_web_view_load_html(WEBKIT_WEB_VIEW(view),
			"<!doctype html><meta name='viewport' content='width=device-width,initial-scale=1'><style>html,body{margin:0;height:100%;font:16px sans-serif}main{padding:16px}</style><main>Let your agents use the models you set up in magpie</main>", NULL);
	}
	guint timeout = g_timeout_add_seconds(15, timed_out, NULL);
	g_main_loop_run(loop);
	if (!timeout_fired) g_source_remove(timeout);
	for (int i = 0; i < 2; i++) gtk_widget_destroy(windows[i]);
	g_main_loop_unref(loop);
	return failed;
}
