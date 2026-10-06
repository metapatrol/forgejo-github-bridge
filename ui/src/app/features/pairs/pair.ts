import {
  ChangeDetectionStrategy,
  Component,
  computed,
  input,
  numberAttribute,
} from '@angular/core';
import { RouterLink } from '@angular/router';
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
import { autoRefresh } from '../../core/refresh';
import { KindLabel, PullFlags, SideLabel, StateBadge } from '../../shared/badges';
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
    KindLabel,
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
  }
}
