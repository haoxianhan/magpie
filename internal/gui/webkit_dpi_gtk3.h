#include <gtk/gtk.h>
#include <math.h>

// WebKitGTK can turn GDK's unset resolution (-1) into a negative font
// scale. Supply a font DPI before it creates any WebView, on the UI thread.
static gboolean prepare_native_webkit(void) {
	if (!gtk_init_check(NULL, NULL))
		return FALSE;
	GdkScreen *screen = gdk_screen_get_default();
	double dpi = gdk_screen_get_resolution(screen);
	if (isfinite(dpi) && dpi > 0)
		return TRUE;

	gint configured = -1;
	g_object_get(gtk_settings_get_default(), "gtk-xft-dpi", &configured, NULL);
	dpi = configured > 0 ? configured / 1024.0 : 96.0;
	const char *scale_env = g_getenv("GDK_DPI_SCALE");
	if (scale_env) {
		char *end;
		double scale = g_ascii_strtod(scale_env, &end);
		if (end != scale_env && *end == '\0' && isfinite(scale) && scale > 0
			&& isfinite(dpi * scale) && dpi * scale > 0)
			dpi *= scale;
	}
	// GDK_SCALE is a separate device scale; GTK already applies it.
	gdk_screen_set_resolution(screen, dpi);
	return TRUE;
}
