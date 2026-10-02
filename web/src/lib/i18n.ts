import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "../locales/en.json";

// All UI strings go through translation keys from day one (ADR-020).
// To add a language: copy locales/en.json, translate it, and register it here.
i18n.use(initReactI18next).init({
  resources: { en: { translation: en } },
  lng: "en",
  fallbackLng: "en",
  interpolation: { escapeValue: false }, // React escapes already
});

export default i18n;
