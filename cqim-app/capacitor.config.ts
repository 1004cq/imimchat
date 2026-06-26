import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'com.imim.chat',
  appName: 'imimchat',
  webDir: 'dist/public',
  server: {
    androidScheme: 'https',
    url: 'https://wed.imim.chat',
    cleartext: false,
  },
  android: {
    allowMixedContent: true,
    captureInput: true,
    webContentsDebuggingEnabled: false,
  },
  plugins: {
    SplashScreen: {
      launchShowDuration: 2000,
      backgroundColor: '#FAFAF8',
      showSpinner: false,
    },
  },
};

export default config;
