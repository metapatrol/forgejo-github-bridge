import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Side } from '../api/models/side';

/** Where a contributor's page is. */
export function contributorLink(side: Side, login: string): string[] {
  return ['/contributors', side, login];
}

/** Someone who opened a pair, linked to their page. */
@Component({
  selector: 'app-author',
  imports: [RouterLink],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<a
    [routerLink]="link()"
    class="mp-link font-mono"
    [title]="login() + ' on ' + (side() === 'github' ? 'GitHub' : 'Forgejo')"
    >{{ login() }}</a
  >`,
  host: { class: 'contents' },
})
export class Author {
  readonly login = input.required<string>();
  readonly side = input.required<Side>();
  protected link(): string[] {
    return contributorLink(this.side(), this.login());
  }
}
