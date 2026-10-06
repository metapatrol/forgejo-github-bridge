import { ChangeDetectionStrategy, Component } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpPageHeader } from '@metapatrol/ng';

@Component({
  selector: 'app-not-found',
  imports: [RouterLink, MpPageHeader],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <mp-page-header title="Not found" />
    <p class="text-sm text-fg-muted">
      There is no such page. <a routerLink="/" class="mp-link">Back to the overview</a>.
    </p>
  `,
})
export class NotFound {}
