import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { MpPill } from '@metapatrol/ng';
import { Pair } from '../api/models/pair';
import { PairKind } from '../api/models/pair-kind';
import { PairState } from '../api/models/pair-state';
import { Side } from '../api/models/side';

/** A pair's state: open in the signal tone, merged ok, closed neutral. */
@Component({
  selector: 'app-state',
  imports: [MpPill],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<mp-pill [tone]="tone()" [tip]="tip()">{{ state() }}</mp-pill>`,
})
export class StateBadge {
  readonly state = input.required<PairState>();
  protected readonly tone = computed(
    () => (({ open: 'info', merged: 'ok', closed: 'neutral' }) as const)[this.state()],
  );
  protected readonly tip = computed(
    () =>
      ({
        open: 'Open on both sides.',
        merged: 'Merged in Forgejo.',
        closed: 'Closed on both sides.',
      })[this.state()],
  );
}

/** Which forge something came from. */
@Component({
  selector: 'app-side',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'font-mono text-xs whitespace-nowrap text-fg-muted' },
  template: `{{ side() === 'github' ? 'GitHub' : 'Forgejo' }}`,
})
export class SideLabel {
  readonly side = input.required<Side>();
}

/** Issue or pull request, as a word with an icon. */
@Component({
  selector: 'app-kind',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'font-mono text-xs whitespace-nowrap text-fg-muted' },
  template: `{{ kind() === 'pull' ? 'pull request' : 'issue' }}`,
})
export class KindLabel {
  readonly kind = input.required<PairKind>();
}

/** What a pull request pair has on top of its state, as pills; nothing for an issue. */
@Component({
  selector: 'app-pull-flags',
  imports: [MpPill],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'inline-flex flex-wrap gap-1' },
  template: `
    @if (pair().pull; as p) {
      @if (p.headFrozen) {
        <mp-pill
          tone="warn"
          tip="Someone else pushed to the contrib branch; the bridge no longer updates it."
          >frozen</mp-pill
        >
      }
      @if (p.touchesCi) {
        <mp-pill tone="warn" tip="Changes CI configuration: read it before approving a run."
          >touches CI</mp-pill
        >
      }
      @if (p.rebaseRequested) {
        <mp-pill tone="info" tip="The bridge asks the contributor to rebase."
          >rebase requested</mp-pill
        >
      }
      @if (pair().state !== 'open') {
        @if (p.outcomeReported) {
          <mp-pill tone="ok" tip="The merge or close has been reported on GitHub."
            >reported</mp-pill
          >
        } @else {
          <mp-pill tone="warn" tip="The merge or close has not been reported on GitHub yet."
            >report pending</mp-pill
          >
        }
      }
    }
  `,
})
export class PullFlags {
  readonly pair = input.required<Pair>();
}
