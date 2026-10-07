import {
  ChangeDetectionStrategy,
  Component,
  computed,
  DestroyRef,
  effect,
  inject,
  input,
  signal,
  untracked,
} from '@angular/core';
import { MpLoadState, MpPageHeader, mpQueryParams } from '@metapatrol/ng';
import { ListActivity$Params, listActivity } from '../../api/fn/status/list-activity';
import { Side } from '../../api/models/side';
import { ACTIVITY_ACTIONS, ACTIVITY_KINDS, ACTIVITY_SORTS } from '../../core/activity';
import { liveReload } from '../../core/events';
import { pagedList, sortParam } from '../../core/paged';
import { ActivityTable } from '../../shared/activity-table';
import { RepositoryInput } from '../../shared/repository-input';

/** Scrolled further than this, new entries are counted in a pill rather than just shown. */
const SCROLLED = 200;

/** The value of a select if it is one of its options. */
function oneOf(value: string | undefined, options: { value: string }[]): string | undefined {
  return options.some((o) => o.value === value) ? value : undefined;
}

/** What the bridge wrote, newest first, as it happens. */
@Component({
  selector: 'app-activity',
  imports: [MpLoadState, MpPageHeader, ActivityTable, RepositoryInput],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './activity.html',
})
export class Activity {
  // The filters live in the URL, so a filtered list can be linked to.
  readonly repository = input<string>();
  readonly side = input<string>();
  readonly kind = input<string>();
  readonly action = input<string>();
  /** ?sort=: a column, descending with a leading -; the newest first by default. */
  readonly sort = input<string>();

  protected readonly setQuery = mpQueryParams();
  protected readonly kinds = ACTIVITY_KINDS;
  protected readonly actions = ACTIVITY_ACTIONS;

  protected readonly params = computed(() => ({
    repository: this.repository() || undefined,
    side: this.side() === 'github' || this.side() === 'forgejo' ? (this.side() as Side) : undefined,
    kind: oneOf(this.kind(), ACTIVITY_KINDS),
    action: oneOf(this.action(), ACTIVITY_ACTIONS),
    sort: sortParam<NonNullable<ListActivity$Params['sort']>>(this.sort(), ACTIVITY_SORTS),
  }));
  protected readonly activity = pagedList(listActivity, () => this.params());

  /**
   * Entries that arrived while the reader was further down the list. The
   * browser keeps the rows they are reading in place as new ones come in
   * above (scroll anchoring); the pill says how many came, and takes them up.
   */
  protected readonly fresh = signal(0);
  private seen = new Set<number>();

  constructor() {
    liveReload(() => ({ view: 'activity' }), this.activity);
    effect(() => {
      const items = this.activity.items();
      untracked(() => {
        const sortedByTime = !this.params().sort || this.params().sort === '-at';
        if (this.seen.size && sortedByTime && window.scrollY > SCROLLED) {
          this.fresh.update((n) => n + items.filter((e) => !this.seen.has(e.id)).length);
        }
        this.seen = new Set(items.map((e) => e.id));
      });
    });
    // New filters, a new list: nothing in it is news.
    effect(() => {
      this.params();
      untracked(() => {
        this.seen = new Set();
        this.fresh.set(0);
      });
    });
    const scrolled = () => {
      if (window.scrollY <= SCROLLED) {
        this.fresh.set(0);
      }
    };
    window.addEventListener('scroll', scrolled, { passive: true });
    inject(DestroyRef).onDestroy(() => window.removeEventListener('scroll', scrolled));
  }

  protected toTop(): void {
    window.scrollTo({ top: 0, behavior: 'smooth' });
    this.fresh.set(0);
  }

  protected filter(name: string, value: string): void {
    this.setQuery({ [name]: value || null });
  }
}
