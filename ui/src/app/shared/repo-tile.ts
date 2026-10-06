import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { RouterLink } from '@angular/router';
import { MpTime } from '@metapatrol/ng';
import { Repository } from '../api/models/repository';
import { repositoryLinks } from '../core/links';
import { QUIET_LINK, TILE, TILE_MAIN } from './tile';
import { lastPoll, repoHealth } from '../core/repos';

/**
 * The grid the tiles go in: tiles of one narrow width, left-aligned, as many
 * per row as fit; one column on a phone. A fixed track rather than
 * minmax(…, 20rem): auto-fill counts columns by the maximum, which leaves a
 * 1024px screen with one.
 */
export const REPO_GRID = 'grid grid-cols-1 gap-3 sm:grid-cols-[repeat(auto-fill,19rem)]';

/** A repository as a narrow tile: its health, its pairs, when it was last polled. */
@Component({
  selector: 'app-repo-tile',
  imports: [RouterLink, MpTime],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'block h-full min-w-0' },
  template: `
    <div [class]="tile + ' flex h-full flex-col gap-1 px-4 py-3'">
      <span class="flex min-w-0 items-center gap-2">
        <span
          class="mp-status shrink-0"
          [attr.data-status]="health().status"
          aria-hidden="true"
        ></span>
        <a
          [routerLink]="links().page.commands"
          [class]="main + ' truncate font-mono text-sm font-medium'"
          [title]="repo().key"
          >{{ name() || repo().key }}</a
        >
      </span>
      <span
        class="font-mono text-xs"
        [class.text-error]="health().status === 'error'"
        [class.text-fg-muted]="health().status !== 'error'"
      >
        @if (repo().queue.depth > 0) {
          <a [routerLink]="queue().commands" [queryParams]="queue().query" [class]="quiet">{{
            health().text
          }}</a>
        } @else {
          {{ health().text }}
        }
      </span>
      <span class="font-mono text-xs text-fg-muted"
        >issues
        <a [routerLink]="'/issues'" [queryParams]="links().issuesOpen.query" [class]="quiet"
          >{{ repo().counts.issuesOpen }} open</a
        >
        ·
        <a [routerLink]="'/issues'" [queryParams]="links().issuesClosed.query" [class]="quiet"
          >{{ repo().counts.issuesClosed }} closed</a
        ></span
      >
      @if (repo().pullRequests) {
        <span class="font-mono text-xs text-fg-muted"
          >PRs
          <a [routerLink]="'/pulls'" [queryParams]="links().pullsOpen.query" [class]="quiet"
            >{{ repo().counts.pullsOpen }} open</a
          >
          ·
          <a [routerLink]="'/pulls'" [queryParams]="links().pullsMerged.query" [class]="quiet"
            >{{ repo().counts.pullsMerged }} merged</a
          >
          ·
          <a [routerLink]="'/pulls'" [queryParams]="links().pullsClosed.query" [class]="quiet"
            >{{ repo().counts.pullsClosed }} closed</a
          ></span
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
    </div>
  `,
})
export class RepoTile {
  readonly repo = input.required<Repository>();
  /** What to call it; the full key by default. */
  readonly name = input('');

  protected readonly tile = TILE;
  protected readonly main = TILE_MAIN;
  protected readonly quiet = QUIET_LINK;
  protected readonly links = computed(() => repositoryLinks(this.repo().key));
  /** The repository's queue: only what fails, when something does. */
  protected readonly queue = computed(() =>
    this.repo().queue.failing > 0 ? this.links().queueFailing : this.links().queue,
  );
  protected readonly health = computed(() => repoHealth(this.repo()));
  protected readonly polled = computed(() => lastPoll(this.repo()));
}
