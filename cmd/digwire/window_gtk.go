//go:build linux && cgo
// +build linux,cgo

package main

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>
#include <stdlib.h>

static void on_destroy(GtkWidget* widget, gpointer data) {
    gtk_main_quit();
}

static int launch_gtk_window(const char* url, const char* icon_path) {
    g_set_prgname("digwire");
    g_set_application_name("Digwire");
    
    if (!gtk_init_check(NULL, NULL)) {
        return 0;
    }

    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(window), "Digwire");
    gtk_window_set_default_size(GTK_WINDOW(window), 980, 720);
    gtk_window_set_position(GTK_WINDOW(window), GTK_WIN_POS_CENTER);

    // Set application icon from theme or file
    gtk_window_set_icon_name(GTK_WINDOW(window), "digwire");
    if (icon_path != NULL && icon_path[0] != '\0') {
        GError *err = NULL;
        gtk_window_set_icon_from_file(GTK_WINDOW(window), icon_path, &err);
        if (err) {
            g_error_free(err);
        }
    }

    // Set GTK dark theme
    GtkSettings *settings = gtk_settings_get_default();
    if (settings != NULL) {
        g_object_set(settings, "gtk-application-prefer-dark-theme", TRUE, NULL);
    }

    // Create WebKit View
    WebKitWebView *webView = WEBKIT_WEB_VIEW(webkit_web_view_new());
    webkit_web_view_load_uri(webView, url);

    // Enable WebKit developer tools
    WebKitSettings *wkSettings = webkit_web_view_get_settings(webView);
    if (wkSettings != NULL) {
        webkit_settings_set_enable_developer_extras(wkSettings, TRUE);
        webkit_settings_set_enable_page_cache(wkSettings, TRUE);
    }

    GdkRGBA bg = {0.14, 0.14, 0.14, 1.0};
    webkit_web_view_set_background_color(webView, &bg);

    gtk_container_add(GTK_CONTAINER(window), GTK_WIDGET(webView));
    g_signal_connect(window, "destroy", G_CALLBACK(on_destroy), NULL);

    gtk_widget_show_all(window);
    gtk_main();
    return 1;
}
*/
import "C"
import (
	"os"
	"path/filepath"
	"runtime"
	"unsafe"
)

func runNativeGTKWindow(url string) bool {
	runtime.LockOSThread()

	iconPath := "/usr/share/icons/hicolor/512x512/apps/digwire.png"
	if _, err := os.Stat(iconPath); err != nil {
		home, _ := os.UserHomeDir()
		iconPath = filepath.Join(home, ".local/share/icons/hicolor/512x512/apps/digwire.png")
	}
	if _, err := os.Stat(iconPath); err != nil {
		iconPath = "assets/digwire.png"
	}

	cURL := C.CString(url)
	defer C.free(unsafe.Pointer(cURL))
	cIcon := C.CString(iconPath)
	defer C.free(unsafe.Pointer(cIcon))

	res := C.launch_gtk_window(cURL, cIcon)
	return int(res) != 0
}
