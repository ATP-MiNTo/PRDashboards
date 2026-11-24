export function token(css: string, name: string): string;
export function png(size: number, pixels: Buffer): Buffer;
export function icon(size: number, bg: string, fg: string, maskable: boolean): Buffer;
export function manifest(themeColor: string, backgroundColor: string): Record<string, unknown>;
