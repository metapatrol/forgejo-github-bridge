import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Pager } from './pager';

describe('Pager', () => {
  let fixture: ComponentFixture<Pager>;

  beforeEach(() => {
    fixture = TestBed.createComponent(Pager);
  });

  function render(offset: number, shown: number, total: number) {
    fixture.componentRef.setInput('offset', offset);
    fixture.componentRef.setInput('limit', 50);
    fixture.componentRef.setInput('shown', shown);
    fixture.componentRef.setInput('total', total);
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    const [prev, next] = Array.from(el.querySelectorAll('button'));
    return { text: el.querySelector('span')?.textContent?.trim(), prev, next };
  }

  it('names the rows shown and the total', () => {
    const { text, prev, next } = render(50, 50, 230);
    expect(text).toBe('51–100 of 230');
    expect(prev.disabled).toBe(false);
    expect(next.disabled).toBe(false);
  });

  it('has nowhere to go on a single page', () => {
    const { text, prev, next } = render(0, 3, 3);
    expect(text).toBe('1–3 of 3');
    expect(prev.disabled).toBe(true);
    expect(next.disabled).toBe(true);
  });

  it('says so when nothing matches', () => {
    expect(render(0, 0, 0).text).toBe('0 of 0');
  });
});
