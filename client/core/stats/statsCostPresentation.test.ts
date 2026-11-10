import { describe, expect, test } from 'bun:test';
import { costEstimateMark, statsModelHeading, statsModelKey } from './statsCostPresentation';
import type { ModelStat } from './statsPayload';

const model = (overrides: Partial<ModelStat> = {}): ModelStat => ({
  name: 'gpt-6-astra', id: 'gpt-6-astra', provider: 'openai', cost: 2,
  costKnown: true, costProvenance: 'estimated', totalTokens: 100,
  inputTokens: 80, outputTokens: 20, reasoningTokens: 0, cacheRead: 0,
  cacheCreate: 0, sessions: 1, ...overrides,
});

describe('Stats overview and model details', () => {
  test('estimated and mixed amounts remain distinct from reported and missing prices', () => {
    expect(costEstimateMark(2, true, 'estimated')).toBe('≈');
    expect(costEstimateMark(2, true, 'mixed')).toBe('≈');
    expect(costEstimateMark(2, true, 'reported')).toBe('');
    expect(costEstimateMark(0, false, 'unknown')).toBe('');
    expect(costEstimateMark(0, false, 'estimated')).toBe('');
    expect(costEstimateMark(0, true, 'estimated')).toBe('≈');
    expect(costEstimateMark(0, true, 'reported')).toBe('');
    expect(costEstimateMark(2, false, 'mixed')).toBe('≈');
  });
  test('provider appears only when it disambiguates the same exact model', () => {
    const first = model();
    const other = model({ provider: 'other-provider' });
    expect(statsModelHeading(first, [first])).toBe('gpt-6-astra');
    expect(statsModelHeading(first, [first, other])).toBe('gpt-6-astra (openai)');
    expect(statsModelKey(first)).not.toBe(statsModelKey(other));
  });
});
