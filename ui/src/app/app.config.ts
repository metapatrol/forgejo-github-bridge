import { provideHttpClient, withFetch } from '@angular/common/http';
import { ApplicationConfig, provideBrowserGlobalErrorListeners } from '@angular/core';
import { provideRouter, withComponentInputBinding } from '@angular/router';
import { provideMetapatrol, provideMpIcons } from '@metapatrol/ng';
import { provideApiConfiguration } from './api/api-configuration';
import { routes } from './app.routes';
import { ICONS } from './shared/icons';

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    // the sidebar and the theme are remembered as forgejo-github-bridge.*
    provideMetapatrol({
      storagePrefix: 'forgejo-github-bridge',
      serviceName: 'the bridge',
    }),
    provideMpIcons(ICONS),
    provideRouter(routes, withComponentInputBinding()),
    provideHttpClient(withFetch()),
    // The API is served by the same listener that serves the console.
    provideApiConfiguration('/api/v1'),
  ],
};
