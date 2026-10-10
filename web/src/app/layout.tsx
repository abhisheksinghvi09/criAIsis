import type { Metadata } from "next";
import { DM_Sans, JetBrains_Mono } from "next/font/google";
import { SessionProvider } from "@/lib/session";
import { ThemeProvider } from "@/lib/theme";
import "./theme.css";
import "./components.css";

// DM Sans carries the interface; JetBrains Mono carries technical values and
// evidence, per the approved design contract.
const dmSans = DM_Sans({ subsets: ["latin"], weight: ["400", "500", "600", "700"], variable: "--font-dm-sans" });
const jetbrains = JetBrains_Mono({ subsets: ["latin"], weight: ["400", "500"], variable: "--font-jetbrains" });

export const metadata: Metadata = {
  title: "criAIsis",
  description: "Read-only incident investigation. Four specialists debate; one commander decides.",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    // data-theme is set to light here and updated by ThemeProvider on mount, so
    // first use is the approved light mode with no flash.
    <html lang="en" data-theme="light" className={`${dmSans.variable} ${jetbrains.variable}`}>
      <body>
        <ThemeProvider>
          <SessionProvider>{children}</SessionProvider>
        </ThemeProvider>
      </body>
    </html>
  );
}
