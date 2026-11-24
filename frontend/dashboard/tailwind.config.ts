import type { Config } from "tailwindcss";

const config: Config = {
  content: [
    "./app/**/*.{ts,tsx}",
    "./components/**/*.{ts,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        navy: {
          950: "#070C18",
          900: "#0B1220",
          850: "#0E1728",
          800: "#131E33",
          700: "#1A2945",
          600: "#23385C",
          500: "#2F4A7A",
          400: "#41609B",
          300: "#6C8BBE",
        },
        accent: {
          DEFAULT: "#10B981",
          soft: "#34D399",
          pale: "#A7F3D0",
          deep: "#059669",
        },
      },
      borderRadius: {
        card: "12px",
        pill: "999px",
      },
      boxShadow: {
        card: "0 6px 28px -8px rgba(0, 0, 0, 0.5), 0 2px 6px -2px rgba(0, 0, 0, 0.25)",
        glow: "0 0 22px -4px rgba(16, 185, 129, 0.45)",
        "glow-soft": "0 4px 18px -6px rgba(16, 185, 129, 0.35)",
      },
      fontFamily: {
        sans: [
          "Inter",
          "Noto Sans JP",
          "Hiragino Kaku Gothic ProN",
          "system-ui",
          "sans-serif",
        ],
      },
    },
  },
  plugins: [],
};

export default config;
