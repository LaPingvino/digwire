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

static void on_script_message(WebKitUserContentManager *manager, WebKitJavascriptResult *res, gpointer user_data) {
    GtkWindow *window = GTK_WINDOW(user_data);
    JSCValue *val = webkit_javascript_result_get_js_value(res);
    if (val && jsc_value_is_string(val)) {
        char *str = jsc_value_to_string(val);
        if (str) {
            if (strcmp(str, "minimize") == 0) {
                gtk_window_iconify(window);
            } else if (strcmp(str, "maximize") == 0) {
                gtk_window_maximize(window);
            } else if (strcmp(str, "unmaximize") == 0) {
                gtk_window_unmaximize(window);
            } else if (strcmp(str, "toggle_maximize") == 0) {
                if (gtk_window_is_maximized(window)) {
                    gtk_window_unmaximize(window);
                } else {
                    gtk_window_maximize(window);
                }
            } else if (strcmp(str, "fullscreen") == 0) {
                gtk_window_fullscreen(window);
            } else if (strcmp(str, "unfullscreen") == 0) {
                gtk_window_unfullscreen(window);
            } else if (strcmp(str, "close") == 0) {
                gtk_window_close(window);
            }
            g_free(str);
        }
    }
}

typedef struct {
    WebKitWebView *web_view;
    char *uri;
} RetryLoadData;

static gboolean retry_load_cb(gpointer user_data) {
    RetryLoadData *data = (RetryLoadData*)user_data;
    if (data != NULL) {
        if (data->web_view != NULL && data->uri != NULL) {
            webkit_web_view_load_uri(data->web_view, data->uri);
        }
        if (data->uri != NULL) {
            g_free(data->uri);
        }
        g_free(data);
    }
    return G_SOURCE_REMOVE;
}

static gboolean on_load_failed(WebKitWebView *web_view, WebKitLoadEvent load_event, gchar *failing_uri, GError *error, gpointer user_data) {
    if (failing_uri != NULL) {
        RetryLoadData *data = g_new0(RetryLoadData, 1);
        data->web_view = web_view;
        data->uri = g_strdup(failing_uri);
        g_timeout_add(300, retry_load_cb, data);
    }
    return TRUE;
}

static void on_web_process_terminated(WebKitWebView *web_view, WebKitWebProcessTerminationReason reason, gpointer user_data) {
    g_printerr("Digwire WebKit: Web process terminated (reason: %d), retrying...\n", (int)reason);
    webkit_web_view_reload(web_view);
}

static int launch_gtk_window(const char* url, const char* icon_path) {
    // Disable DMA-BUF hardware renderer, broken compositing modes, and sandbox restrictions on Linux GPUs
    setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1", 1);
    setenv("WEBKIT_DISABLE_COMPOSITING_MODE", "1", 1);
    setenv("WEBKIT_FORCE_SANDBOX", "0", 1);
    setenv("WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS", "1", 1);

    g_set_prgname("digwire");
    g_set_application_name("Digwire");
    
    if (!gtk_init_check(NULL, NULL)) {
        return 0;
    }

    // Disable WebKit sandboxing if supported by web context
    WebKitWebContext *ctx = webkit_web_context_get_default();
    if (ctx != NULL) {
        webkit_web_context_set_sandbox_enabled(ctx, FALSE);
    }

    GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
    gtk_window_set_title(GTK_WINDOW(window), "Digwire");
    gtk_window_set_default_size(GTK_WINDOW(window), 980, 720);
    gtk_window_set_position(GTK_WINDOW(window), GTK_WIN_POS_CENTER);
    gtk_window_set_resizable(GTK_WINDOW(window), TRUE);
    gtk_window_set_type_hint(GTK_WINDOW(window), GDK_WINDOW_TYPE_HINT_NORMAL);

    // Set application icon from theme or file
    gtk_window_set_icon_name(GTK_WINDOW(window), "digwire");
    if (icon_path != NULL && icon_path[0] != '\0') {
        GError *err = NULL;
        gtk_window_set_icon_from_file(GTK_WINDOW(window), icon_path, &err);
        if (err) {
            g_error_free(err);
        }
    }

    // Set GTK dark theme and window decorations
    GtkSettings *settings = gtk_settings_get_default();
    if (settings != NULL) {
        g_object_set(settings, "gtk-application-prefer-dark-theme", TRUE, NULL);
        g_object_set(settings, "gtk-decoration-layout", "menu:minimize,maximize,close", NULL);
    }

    // Setup script message handler for window controls
    WebKitUserContentManager *ucm = webkit_user_content_manager_new();
    webkit_user_content_manager_register_script_message_handler(ucm, "windowAction");
    g_signal_connect(ucm, "script-message-received::windowAction", G_CALLBACK(on_script_message), window);

    // Create WebKit View with ucm
    WebKitWebView *webView = WEBKIT_WEB_VIEW(webkit_web_view_new_with_user_content_manager(ucm));

    // Configure WebKit settings to prevent black screens and ensure fast rendering
    WebKitSettings *wkSettings = webkit_web_view_get_settings(webView);
    if (wkSettings != NULL) {
        webkit_settings_set_enable_developer_extras(wkSettings, TRUE);
        webkit_settings_set_enable_page_cache(wkSettings, FALSE);
        webkit_settings_set_enable_javascript(wkSettings, TRUE);
        webkit_settings_set_enable_smooth_scrolling(wkSettings, TRUE);
        webkit_settings_set_hardware_acceleration_policy(wkSettings, WEBKIT_HARDWARE_ACCELERATION_POLICY_NEVER);
    }

    // Connect load failure and process termination handlers
    g_signal_connect(webView, "load-failed", G_CALLBACK(on_load_failed), NULL);
    g_signal_connect(webView, "web-process-terminated", G_CALLBACK(on_web_process_terminated), NULL);

    webkit_web_view_load_uri(webView, url);

    GdkRGBA bg = {0.141, 0.141, 0.141, 1.0};
    webkit_web_view_set_background_color(webView, &bg);

    gtk_container_add(GTK_CONTAINER(window), GTK_WIDGET(webView));
    g_signal_connect(window, "destroy", G_CALLBACK(on_destroy), NULL);

    gtk_widget_show_all(window);
    gtk_widget_grab_focus(GTK_WIDGET(webView));
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
