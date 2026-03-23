import "./global.css";
import { RootProvider } from "fumadocs-ui/provider/next";
import type { ReactNode } from "react";

export const metadata = {
  title: "LOKA Docs",
  description: "Controlled execution environment for AI agents",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en" className="dark" suppressHydrationWarning>
      <body suppressHydrationWarning>
        <RootProvider enableThemeProvider={false}>{children}</RootProvider>
      </body>
    </html>
  );
}
