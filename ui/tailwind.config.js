/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
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
