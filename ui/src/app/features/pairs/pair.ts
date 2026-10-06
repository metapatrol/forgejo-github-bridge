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
import { repoPath, shortSha } from '../../core/format';
import { KINDS, pairLink } from '../../core/kinds';
import { PairKind } from '../../api/models/pair-kind';
import { autoRefresh } from '../../core/refresh';
import { PullFlags, SideLabel, StateBadge } from '../../shared/badges';
import { QueueTable } from '../../shared/queue-table';

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
    QueueTable,
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
  protected readonly shortSha = shortSha;
  protected readonly timestamp = timestamp;

  constructor() {
    autoRefresh(this.pair);
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
