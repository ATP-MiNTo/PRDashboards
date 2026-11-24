import { createHash } from 'node:crypto';
import { describe, expect, it } from 'vitest';
import { inlineScriptHashes, inlineStyleProblems } from './csp-hashes.mjs';

const sha = (s: string) => `'sha256-${createHash('sha256').update(s).digest('base64')}'`;

describe('inlineScriptHashes', () => {
  it('hashes the exact body of inline scripts', () => {
    const html = '<script>self.__next_f.push([1,"a"])</script><script id="x">1+1</script>';
    expect(inlineScriptHashes(html)).toEqual([sha('self.__next_f.push([1,"a"])'), sha('1+1')]);
  });

  it('skips external scripts', () => {
    expect(inlineScriptHashes('<script src="/a.js" async=""></script>')).toEqual([]);
  });

  it('keeps whitespace and handles empty bodies', () => {
    expect(inlineScriptHashes('<script> x\n</script><script></script>')).toEqual([
      sha(' x\n'),
      sha(''),
    ]);
  });

  it('handles multi-line bodies', () => {
    expect(inlineScriptHashes('<script>\na\nb\n</script>')).toEqual([sha('\na\nb\n')]);
  });
});

describe('inlineStyleProblems', () => {
  it('flags style blocks and attributes', () => {
    expect(inlineStyleProblems('<style>a{}</style>')).toEqual(['<style> block']);
    expect(inlineStyleProblems('<div style="color:red">x</div>')).toEqual(['style attribute']);
  });

  it('accepts stylesheets and plain markup', () => {
    expect(
      inlineStyleProblems('<link rel="stylesheet" href="/a.css"><p class="x">text</p>'),
    ).toEqual([]);
  });
});
