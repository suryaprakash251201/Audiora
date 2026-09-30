import type { CapacitorConfig } from '@capacitor/cli'

/**
 * Capacitor configuration.
 *
 * The same Vite build is the web app and the native shell, so there is one
 * codebase and one set of components. `webDir` points at the Vite output and
 * `cap sync` copies it into the native projects.
 *
 * The API base is baked in at build time via VITE_API_BASE. In production it
 * should be the same origin the app is served from, which lets the Caddy
 * reverse proxy handle TLS and means no CORS configuration is needed.
 */
const config: CapacitorConfig = {
  appId: 'app.audiora.player',
  appName: 'Audiora',
  webDir: 'dist',

  ios: {
    // A music player should keep playing with the screen off, which needs the
    // background audio capability declared in the Xcode target.
    contentInset: 'never',
    backgroundColor: '#07070b',
  },

  android: {
    // Draw behind the status and navigation bars; the CSS safe-area insets
    // handle the padding.
    backgroundColor: '#07070b',
    allowMixedContent: false,
  },

  plugins: {
    StatusBar: {
      style: 'DARK',
      backgroundColor: '#00000000',
      overlaysWebView: true,
    },
    MediaSession: {
      // Tells the OS the app is a media player, which is what enables the
      // lock-screen and notification controls.
      enabled: true,
    },
  },
}

export default config
