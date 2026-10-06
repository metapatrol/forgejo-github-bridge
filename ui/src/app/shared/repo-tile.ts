import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpTime } from '@metapatrol/ng';
import { Repository } from '../api/models/repository';
import { repoPath } from '../core/format';
import { lastPoll, repoHealth } from '../core/repos';

/**
 * The grid the tiles go in: tiles of one narrow width, left-aligned, as many
 * per row as fit; one column on a phone. A fixed track rather than
 * minmax(…, 20rem): auto-fill counts columns by the maximum, which leaves a
 * 1024px screen with one.
 */
export const REPO_GRID = 'grid grid-cols-1 gap-3 sm:grid-cols-[repeat(auto-fill,19rem)]';

/** A tile that is a link as a whole: hover and keyboard focus as the brand's panels take them. */
export const TILE_LINK =
  'mp-panel mp-panel-flush text-fg no-underline hover:border-line-strong hover:bg-surface-2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-signal';

/** A repository as a narrow tile: its health, its pairs, when it was last polled. */
@Component({
  selector: 'app-repo-tile',
  imports: [RouterLink, MpTime],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block h-full min-w-0' },
  template: `
    <a
      [routerLink]="['/repositories', ...path()]"
      [class]="tileLink + ' flex h-full flex-col gap-1 px-4 py-3'"
      [title]="repo().key"
    >
      <span class="flex min-w-0 items-center gap-2">
        <span
          class="mp-status shrink-0"
          [attr.data-status]="health().status"
          aria-hidden="true"
        ></span>
        <span class="truncate font-mono text-sm font-medium">{{ name() || repo().key }}</span>
      </span>
      <span
        class="font-mono text-xs"
        [class.text-error]="health().status === 'error'"
        [class.text-fg-muted]="health().status !== 'error'"
        >{{ health().text }}</span
      >
      <span class="font-mono text-xs text-fg-muted"
        >issues {{ repo().counts.issuesOpen }} open · {{ repo().counts.issuesClosed }} closed</span
      >
      @if (repo().pullRequests) {
        <span class="font-mono text-xs text-fg-muted"
          >PRs {{ repo().counts.pullsOpen }} open · {{ repo().counts.pullsMerged }} merged ·
          {{ repo().counts.pullsClosed }} closed</span
        >
      } @else {
        <span class="font-mono text-xs text-fg-subtle">PRs off</span>
      }
      <span class="truncate font-mono text-xs text-fg-subtle">↔ {{ repo().forgejo }}</span>
      <span class="mt-auto pt-1 text-xs text-fg-subtle">
        @if (polled(); as at) {
          polled <mp-time [value]="at" />
        } @else {
          not polled yet
        }
      </span>
    </a>
  `,
})
export class RepoTile {
  readonly repo = input.required<Repository>();
  /** What to call it; the full key by default. */
  readonly name = input('');

  protected readonly tileLink = TILE_LINK;
  protected readonly path = computed(() => repoPath(this.repo().key));
  protected readonly health = computed(() => repoHealth(this.repo()));
  protected readonly polled = computed(() => lastPoll(this.repo()));
}
