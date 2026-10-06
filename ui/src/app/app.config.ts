import { provideHttpClient, withFetch } from '@angular/common/http';
import { ApplicationConfig, provideBrowserGlobalErrorListeners } from '@angular/core';
import { provideRouter, withComponentInputBinding, withNavigationErrorHandler } from '@angular/router';
import { provideMetapatrol, provideMpIcons } from '@metapatrol/ng';
import { provideApiConfiguration } from './api/api-configuration';
import { routes } from './app.routes';
import { reloadOnStaleBundle } from './core/stale-bundle';
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
    // A tab left open across a deployment asks for chunks the new build no
    // longer has; it reloads onto the new build instead of failing.
    provideRouter(routes, withComponentInputBinding(), withNavigationErrorHandler((e) => reloadOnStaleBundle(e))),
    provideHttpClient(withFetch()),
    // The API is served by the same listener that serves the console.
    provideApiConfiguration('/api/v1'),
  ],
};
