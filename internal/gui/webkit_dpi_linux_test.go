//go:build linux && cgo && gtk3 && !nogui

package gui

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// GTK and WebKit cache process-wide settings. Each case uses a native child
// with an isolated home and the same compatibility helper as the desktop app.
func TestGTK3WebkitDPI(t *testing.T) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Skip("native GTK3/WebKit test needs a display")
	}
	flags, err := exec.Command("pkg-config", "--cflags", "--libs", "gtk+-3.0", "webkit2gtk-4.1").Output()
	if err != nil {
		t.Fatalf("native probe dependencies: %v", err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "probe.c")
	if err := os.WriteFile(source, []byte(gtkDPIProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	include, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := append([]string{source, "-I" + include, "-o", bin}, strings.Fields(string(flags))...)
	if out, err := exec.Command("cc", args...).CombinedOutput(); err != nil {
		t.Fatalf("build native probe: %v\n%s", err, out)
	}
	for _, tc := range []struct {
		name       string
		gdk, gtk   int
		scale      string
		device     string
		zoom, want float64
	}{
		{"unset", -1, -1, "1", "1", 1, 96},
		{"zero", 0, -1, "1", "1", 1, 96},
		{"configured", -1, 144 * 1024, "1", "1", 1, 144},
		{"existing", 120, 144 * 1024, "1.25", "1", 1, 120},
		{"font-scale", -1, -1, "1.25", "1", 1, 120},
		{"configured-scale", -1, 144 * 1024, "1.25", "1", 1, 180},
		{"device-scale", -1, -1, "1", "2", 1, 96},
		{"text-zoom", -1, -1, "1", "1", 1.1, 96},
		{"invalid-scale", -1, -1, "garbage", "1", 1, 96},
		{"negative-scale", -1, -1, "-1", "1", 1, 96},
		{"zero-scale", -1, -1, "0", "1", 1, 96},
		{"nonfinite-scale", -1, -1, "nan", "1", 1, 96},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			config := filepath.Join(home, ".config", "gtk-3.0")
			if err := os.MkdirAll(config, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(config, "settings.ini"), []byte(fmt.Sprintf("[Settings]\ngtk-xft-dpi=%d\n", tc.gtk)), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, bin, strconv.Itoa(tc.gdk), fmt.Sprint(tc.zoom))
			cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"), "XDG_CACHE_HOME="+filepath.Join(home, ".cache"), "XDG_DATA_HOME="+filepath.Join(home, ".local", "share"), "XDG_STATE_HOME="+filepath.Join(home, ".local", "state"), "GSETTINGS_BACKEND=memory", "GDK_DPI_SCALE="+tc.scale, "GDK_SCALE="+tc.device, "WEBKIT_DISABLE_COMPOSITING_MODE=1")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("native WebKit probe: %v\n%s", err, out)
			}
			seen := 0
			for _, line := range strings.Split(string(out), "\n") {
				var id, allocated, device, initialDevice int
				var dpi, width, height, content, font float64
				if n, _ := fmt.Sscanf(line, "layout %d %f %f %f %f %f %d %d %d", &id, &dpi, &width, &height, &content, &font, &allocated, &device, &initialDevice); n != 9 {
					continue
				}
				seen++
				t.Log(line)
				for _, v := range []float64{width, height, content, font} {
					if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
						t.Fatalf("unusable native layout: %s", line)
					}
				}
				if dpi != tc.want || device != initialDevice {
					t.Errorf("DPI/device scale changed: %s; want DPI %g and initial device scale", line, tc.want)
				}
				wantWidth := float64(allocated) * 96 / tc.want / tc.zoom
				if math.Abs(width-wantWidth) > 2 || content < width/2 || math.Abs(font-16) > 0.1 {
					t.Errorf("incorrect content width or scale: %s; want viewport %g", line, wantWidth)
				}
			}
			if seen != 2 {
				t.Fatalf("expected main and panel layout measurements, got %d\n%s", seen, out)
			}
		})
	}
	t.Run("no-display", func(t *testing.T) {
		cmd := exec.Command(bin, "-1", "1")
		cmd.Env = append(os.Environ(), "DISPLAY=", "WAYLAND_DISPLAY=", "GDK_BACKEND=wayland")
		out, err := cmd.CombinedOutput()
		if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 3 {
			t.Fatalf("expected GTK initialization failure (exit 3), got %v\n%s", err, out)
		}
	})
}

const gtkDPIProbe = `
#include "webkit_dpi_gtk3.h"
#include <webkit2/webkit2.h>
#include <stdio.h>
#include <stdlib.h>

static GMainLoop *loop;
static int remaining = 2;
static int failed;
static int initial_device;

static void measured(GObject *view, GAsyncResult *result, gpointer data) {
	GError *error = NULL;
	JSCValue *value = webkit_web_view_evaluate_javascript_finish(WEBKIT_WEB_VIEW(view), result, &error);
	if (!value) {
		fprintf(stderr, "JavaScript: %s\n", error->message);
		g_error_free(error);
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

int main(int argc, char **argv) {
	if (!gtk_init_check(NULL, NULL)) return 3;
	initial_device = gdk_screen_get_monitor_scale_factor(gdk_screen_get_default(), 0);
	// Even a positive settings.ini property can leave GDK unset. Reproduce
	// that state explicitly; application-owned GtkSettings would mask it.
	gdk_screen_set_resolution(gdk_screen_get_default(), atof(argv[1]));
	if (!prepare_native_webkit()) return 3;
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
		webkit_web_view_set_zoom_level(WEBKIT_WEB_VIEW(view), atof(argv[2]));
		gtk_container_add(GTK_CONTAINER(windows[i]), view);
		g_signal_connect(view, "load-changed", G_CALLBACK(loaded), GINT_TO_POINTER(i));
		gtk_widget_show_all(windows[i]);
		webkit_web_view_load_html(WEBKIT_WEB_VIEW(view),
			"<!doctype html><meta name='viewport' content='width=device-width,initial-scale=1'><style>html,body{margin:0;height:100%;font:16px sans-serif}main{padding:16px}</style><main>Let your agents use the models you set up in magpie</main>", NULL);
	}
	g_main_loop_run(loop);
	for (int i = 0; i < 2; i++) gtk_widget_destroy(windows[i]);
	g_main_loop_unref(loop);
	return failed;
}
`
