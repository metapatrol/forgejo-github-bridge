import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  inject,
  input,
  numberAttribute,
} from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import {
  MpClock,
  MpId,
  MpLoadState,
  MpNotice,
  MpPageHeader,
  MpPanel,
  MpTime,
  timestamp,
} from '@metapatrol/ng';
import { getPair } from '../../api/fn/pairs/get-pair';
import { apiResource } from '../../core/api';
import { repoPath, shortSha, until } from '../../core/format';
import { KINDS, pairLink } from '../../core/kinds';
import { PairKind } from '../../api/models/pair-kind';
import { liveReload } from '../../core/events';
import { pairsLink, queueLink, repositoryLink } from '../../core/links';
import { PullFlags, SideLabel, StateBadge } from '../../shared/badges';
import { Author } from '../../shared/author';
import { history, HistoryEntry, sideName } from '../../core/history';

@Component({
  selector: 'app-pair',
  imports: [
    RouterLink,
    MpId,
    MpLoadState,
    MpNotice,
    MpPageHeader,
    MpPanel,
    MpTime,
    SideLabel,
    StateBadge,
    PullFlags,
    Author,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './pair.html',
})
export class PairDetailPage {
  readonly id = input.required({ transform: (v: unknown) => numberAttribute(v) });
  /** From the route: which list this page belongs to. */
  readonly kind = input.required<PairKind>();
  protected readonly list = computed(() => KINDS[this.kind()]);
  protected readonly pair = apiResource(getPair, () =>
    Number.isFinite(this.id()) ? { id: this.id() } : undefined,
  );
  protected readonly p = computed(() => this.pair.body());
  protected readonly title = computed(() => {
    const p = this.p();
    return p ? p.title || `#${p.githubNumber}` : `pair ${this.id()}`;
  });
  protected readonly repoPath = repoPath;
  protected readonly sideName = sideName;
  protected readonly repoLink = computed(() => repositoryLink(this.p()?.repository ?? ''));
  /** The list of this pair's kind, in its repository and state. */
  protected readonly stateLink = computed(() => {
    const p = this.p();
    return pairsLink(this.kind(), { repository: p?.repository, state: p?.state ?? 'open' });
  });
  /** The queue of this pair's repository. */
  protected readonly queueLink = computed(() =>
    queueLink({ repository: this.p()?.repository, failing: !!this.p()?.queued?.lastError }),
  );
  /** What the bridge holds about the pair, as a timeline. */
  protected readonly entries = computed(() => {
    const p = this.p();
    return p ? history(p) : [];
  });
  private readonly clock = inject(MpClock);

  protected due(at: string): string {
    return until(at, this.clock.now());
  }

  /** The dot of a history entry. */
  protected dot(e: HistoryEntry): string {
    if (e.kind === 'queued') {
      return this.p()?.queued?.lastError ? 'error' : 'active';
    }
    return e.kind === 'paired' ? 'success' : e.kind === 'change' ? 'idle' : 'info';
  }
  protected readonly shortSha = shortSha;
  protected readonly timestamp = timestamp;

  constructor() {
    liveReload(() => ({ view: 'pair', id: this.id() }), this.pair);
    // An issue opened as a pull request's page, or the other way round, goes
    // where it belongs.
    const router = inject(Router);
    effect(() => {
      const p = this.p();
      if (p && p.kind !== this.kind()) {
        void router.navigate(pairLink(p.kind, p.id), { replaceUrl: true });
      }
    });
  }
}
