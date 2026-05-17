/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      colors: {
        lantern: {
          // The Cave: Backgrounds & Surfaces
          abyss: "#000000",
          charcoal: "#0D0D0D",
          illumination: "#1A1A1A",
          cave: "#2D2D2D",
          // The Lantern: Accents & Data Visualization
          ember: "#FF5722",
          glow: "#FF8A65",
          flicker: "#FFCCBC",
          heat: "#E64A19",
          emberGlow: "#FF7043",
          success: "#4ADE80",
          warning: "#FBBF24",
          // Illumination: Typography
          pure: "#FFFFFF",
          mid: "#C9C9D2",
          ash: "#A1A1AA",
        },
      },
      animation: {
        "pulse-slow": "pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite",
        "glow-ember": "glow-ember 2s ease-in-out infinite alternate",
      },
      keyframes: {
        "glow-ember": {
          "0%": { boxShadow: "0 0 4px rgba(255, 87, 34, 0.2)" },
          "100%": { boxShadow: "0 0 12px rgba(255, 87, 34, 0.5)" },
        },
      },
    },
  },
  plugins: [],
};
