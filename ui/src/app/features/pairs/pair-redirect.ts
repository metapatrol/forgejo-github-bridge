import {
  ChangeDetectionStrategy,
  Component,
  effect,
  inject,
  input,
  numberAttribute,
} from '@angular/core';
import { Router } from '@angular/router';
import { MpLoadState } from '@metapatrol/ng';
import { getPair } from '../../api/fn/pairs/get-pair';
import { apiResource } from '../../core/api';
import { pairLink } from '../../core/kinds';

/** /pairs/:id, from before issues and pull requests had pages of their own. */
@Component({
  selector: 'app-pair-redirect',
  imports: [MpLoadState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `<mp-load-state
    [error]="pair.error()"
    [loading]="pair.isLoading()"
    what="pair"
    [name]="id().toString()"
    back="/issues"
  />`,
})
export class PairRedirect {
  readonly id = input.required({ transform: (v: unknown) => numberAttribute(v) });
  protected readonly pair = apiResource(getPair, () =>
    Number.isFinite(this.id()) ? { id: this.id() } : undefined,
  );

  constructor() {
    const router = inject(Router);
    effect(() => {
      const p = this.pair.body();
      if (p) {
        void router.navigate(pairLink(p.kind, p.id), { replaceUrl: true });
      }
    });
  }
}
