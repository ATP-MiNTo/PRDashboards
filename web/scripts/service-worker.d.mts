export const OFFLINE: string;
export const NEVER: string[];
export function urlOf(rel: string): string | null;
export function precacheList(files: string[]): string[];
export function worker(version: string, urls: string[]): string;
