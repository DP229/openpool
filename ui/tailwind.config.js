/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ["./src/**/*.{js,jsx,ts,tsx}", "./index.html"],
  theme: {
    extend: {
      colors: {
        gray: {
          850: "#1f2937",
          900: "#111827",
          950: "#030712",
        }
      }
    }
  },
  plugins: [],
}