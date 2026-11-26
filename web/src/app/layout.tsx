import type { Metadata } from 'next';
import type { ReactNode } from 'react';
import { Providers } from './providers';
import './tailwind.css';
import './globals.css';

export const metadata: Metadata = {
  title: 'PR Dashboards',
  description: 'Open pull requests assigned or opened by you, linked to Jira.',
  referrer: 'no-referrer',
  manifest: '/manifest.webmanifest',
  icons: { icon: '/icons/icon-192.png', apple: '/icons/icon-192.png' },
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
