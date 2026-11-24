"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { DEMO_MODE } from "@/lib/demo";

export type Locale = "ja" | "en";

type LocaleContextValue = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  /** Return the English string when the locale is "en", otherwise Japanese. */
  tr: (ja: string, en: string) => string;
};

const LocaleContext = createContext<LocaleContextValue>({
  locale: "ja",
  setLocale: () => {},
  tr: (ja) => ja,
});

export function LocaleProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>(DEMO_MODE ? "en" : "ja");

  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const q = params.get("lang");
    let saved: string | null = null;
    try {
      saved = window.localStorage.getItem("cryptoengine_locale");
    } catch {
      saved = null;
    }
    const next: Locale =
      q === "en" || q === "ja"
        ? q
        : saved === "en" || saved === "ja"
          ? saved
          : DEMO_MODE
            ? "en"
            : "ja";
    setLocaleState(next);
    document.documentElement.lang = next;
  }, []);

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    try {
      window.localStorage.setItem("cryptoengine_locale", next);
    } catch {
      // ignore (private mode)
    }
    document.documentElement.lang = next;
  }, []);

  const tr = useCallback(
    (ja: string, en: string) => (locale === "en" ? en : ja),
    [locale]
  );

  const value = useMemo(
    () => ({ locale, setLocale, tr }),
    [locale, setLocale, tr]
  );

  return (
    <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>
  );
}

export function useI18n(): LocaleContextValue {
  return useContext(LocaleContext);
}
