import { describe, it, expect } from 'vitest'
import { wordBoundaryAt } from './wordBoundary'

describe('wordBoundaryAt', () => {
  it('selects the alphanumeric word the col is inside', () => {
    expect(wordBoundaryAt('git status -v', 2)).toEqual({ start: 0, len: 3 })
  })
  it('selects the word at the start position', () => {
    expect(wordBoundaryAt('git status -v', 4)).toEqual({ start: 4, len: 6 })
  })
  it('treats a punctuation run as one word', () => {
    expect(wordBoundaryAt('--foo', 1)).toEqual({ start: 0, len: 2 })
  })
  it('returns len=0 when col is on whitespace', () => {
    expect(wordBoundaryAt('hi  there', 2)).toEqual({ start: 2, len: 0 })
  })
  it('handles col at the last character', () => {
    expect(wordBoundaryAt('abc', 2)).toEqual({ start: 0, len: 3 })
  })
  it('returns len=0 when col is past line end', () => {
    expect(wordBoundaryAt('abc', 10)).toEqual({ start: 3, len: 0 })
  })
  it('returns len=0 on an empty line', () => {
    expect(wordBoundaryAt('', 0)).toEqual({ start: 0, len: 0 })
  })
  it('treats a single CJK character as one word', () => {
    expect(wordBoundaryAt('读 hello', 0)).toEqual({ start: 0, len: 1 })
  })
  it('groups underscores and digits into the alnum class', () => {
    expect(wordBoundaryAt('foo_bar123 baz', 5)).toEqual({ start: 0, len: 10 })
  })
  it('does not merge alnum across a leading punctuation prefix', () => {
    // cursor on 'f' of '--foo' should yield just 'foo', not '--foo'
    expect(wordBoundaryAt('--foo', 2)).toEqual({ start: 2, len: 3 })
  })
  it('does not merge punct across a trailing alnum word', () => {
    // cursor on first '-' of 'foo--' should yield '--', not 'foo--'
    expect(wordBoundaryAt('foo--', 3)).toEqual({ start: 3, len: 2 })
  })
  it('returns len=0 for negative col (defensive)', () => {
    expect(wordBoundaryAt('abc', -1)).toEqual({ start: 0, len: 0 })
  })
})
