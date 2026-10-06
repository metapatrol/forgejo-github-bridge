import { DestroyRef, inject, Injectable, signal } from '@angular/core';
import { PairKind } from '../api/models/pair-kind';

/** A change the bridge announces on GET /api/v1/events. Events name what changed, never its content. */
export type BridgeEvent =
  | { kind: 'ready' }
  | { kind: 'resync' }
  | { kind: 'pair'; op: string; id: number; repository?: string; pairKind?: PairKind }
  | { kind: 'queue'; op: string; repository: string; side: string; number: number }
  | { kind: 'cursor'; op: string; repository: string; side: string; stream: string }
  | { kind: 'leader'; present: boolean }
  | { kind: 'rateLimit'; op: string; resource: string }
  | { kind: 'maintenance'; op: string; id: number; finished: boolean };

const KINDS = [
  'ready',
  'resync',
  'pair',
  'queue',
  'cursor',
  'leader',
  'rateLimit',
  'maintenance',
] as const;

/** What a view shows, as far as events are concerned. */
export type Scope =
  | { view: 'status' }
  | { view: 'overview' }
  | { view: 'repositories' }
  | { view: 'repository'; repository: string }
  | { view: 'pairs'; kind: PairKind; repository?: string }
  | { view: 'pair'; id: number }
  | { view: 'queue' }
  | { view: 'contributors' }
  | { view: 'system' }
  | { view: 'contributor'; login: string; side: string };

/**
 * Whether an event changes what a view shows. ready and resync change
 * everything: changes before them were not sent.
 */
export function concerns(e: BridgeEvent, scope: Scope): boolean {
  if (e.kind === 'ready' || e.kind === 'resync') {
    return true;
  }
  switch (scope.view) {
    case 'status':
      return e.kind === 'leader' || e.kind === 'queue' || e.kind === 'rateLimit';
    case 'overview':
      return true;
    case 'repositories':
      return e.kind === 'pair' || e.kind === 'queue' || e.kind === 'cursor';
    case 'repository':
      if (e.kind === 'pair') {
        return !e.repository || e.repository === scope.repository;
      }
      return (e.kind === 'queue' || e.kind === 'cursor') && e.repository === scope.repository;
    case 'pairs':
      if (e.kind === 'pair') {
        return (
          (!e.pairKind || e.pairKind === scope.kind) &&
          (!e.repository || !scope.repository || e.repository === scope.repository)
        );
      }
      return e.kind === 'queue' && (!scope.repository || e.repository === scope.repository);
    case 'pair':
      return (e.kind === 'pair' && e.id === scope.id) || e.kind === 'queue';
    case 'queue':
      return e.kind === 'queue';
    case 'contributors':
      return e.kind === 'pair';
    case 'system':
      return e.kind === 'maintenance';
    case 'contributor':
      // A pair event does not name its author: any may be theirs.
      return e.kind === 'pair' || e.kind === 'queue';
  }
}

/** The state of the stream, for the indicator in the top bar. */
export type StreamState = 'connecting' | 'live' | 'reconnecting';

const MIN_BACKOFF_MS = 1_000;
const MAX_BACKOFF_MS = 30_000;

/**
 * The console's one connection to the bridge's event stream. Views reload
 * what they show when an event concerns them, instead of polling. The
 * stream reconnects with backoff when it drops; every view reloads on the
 * ready that follows, since changes in between were not sent.
 */
@Injectable({ providedIn: 'root' })
export class EventsService {
  private readonly listeners = new Set<(e: BridgeEvent) => void>();
  private source?: EventSource;
  private backoff = MIN_BACKOFF_MS;
  private started = false;

  readonly state = signal<StreamState>('connecting');

  /** Calls fn for every event until the returned function is called. */
  on(fn: (e: BridgeEvent) => void): () => void {
    this.start();
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private start(): void {
    if (this.started || typeof EventSource === 'undefined') {
      return;
    }
    this.started = true;
    this.connect();
  }

  private connect(): void {
    const source = new EventSource('/api/v1/events');
    this.source = source;
    for (const kind of KINDS) {
      source.addEventListener(kind, (m) => {
        const event = parseEvent(kind, (m as MessageEvent<string>).data);
        if (!event) {
          return;
        }
        if (kind === 'ready') {
          this.state.set('live');
          this.backoff = MIN_BACKOFF_MS;
        }
        this.emit(event);
      });
    }
    // EventSource retries on its own at a fixed pace; this one backs off.
    source.onerror = () => {
      source.close();
      if (this.source !== source) {
        return;
      }
      this.state.set('reconnecting');
      setTimeout(() => this.connect(), this.backoff);
      this.backoff = Math.min(this.backoff * 2, MAX_BACKOFF_MS);
    };
  }

  private emit(e: BridgeEvent): void {
    for (const fn of this.listeners) {
      fn(e);
    }
  }
}

/** An event's data as the stream sends it; null if it does not parse. */
export function parseEvent(kind: string, data: string | undefined): BridgeEvent | null {
  if (!(KINDS as readonly string[]).includes(kind)) {
    return null;
  }
  try {
    return { ...(data ? JSON.parse(data) : {}), kind } as BridgeEvent;
  } catch {
    return null;
  }
}

/** fn, once, ms after the last of a burst of calls. */
export function debounce(fn: () => void, ms = 400): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return () => {
    clearTimeout(timer);
    timer = setTimeout(fn, ms);
  };
}

/**
 * Reloads the given resources when an event concerns what the calling view
 * shows; call in an injection context. A reconcile writes several rows, so
 * a burst of events reloads once. reload() keeps what a resource shows
 * while it loads again, so the page does not flicker.
 */
export function liveReload(scope: () => Scope, ...resources: { reload(): boolean }[]): void {
  const reload = debounce(() => resources.forEach((r) => r.reload()));
  const off = inject(EventsService).on((e) => {
    if (concerns(e, scope())) {
      reload();
    }
  });
  inject(DestroyRef).onDestroy(off);
}
