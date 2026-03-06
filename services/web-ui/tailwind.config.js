/** @type {import('tailwindcss').Config} */
export default {
    darkMode: ["class"],
    content: [
        "./index.html",
        "./src/**/*.{js,ts,jsx,tsx}",
    ],
    theme: {
        extend: {
            fontFamily: {
                sans: ['Inter', 'sans-serif'],
            },
            colors: {
                border: "hsl(var(--border))",
                input: "hsl(var(--input))",
                ring: "hsl(var(--ring))",
                background: "hsl(var(--background))",
                foreground: "hsl(var(--foreground))",
                primary: {
                    DEFAULT: "hsl(var(--primary))",
                    foreground: "hsl(var(--primary-foreground))",
                },
                secondary: {
                    DEFAULT: "hsl(var(--secondary))",
                    foreground: "hsl(var(--secondary-foreground))",
                },
                destructive: {
                    DEFAULT: "hsl(var(--destructive))",
                    foreground: "hsl(var(--destructive-foreground))",
                },
                muted: {
                    DEFAULT: "hsl(var(--muted))",
                    foreground: "hsl(var(--muted-foreground))",
                },
                accent: {
                    DEFAULT: "hsl(var(--accent))",
                    foreground: "hsl(var(--accent-foreground))",
                },
                popover: {
                    DEFAULT: "hsl(var(--popover))",
                    foreground: "hsl(var(--popover-foreground))",
                },
                card: {
                    DEFAULT: "hsl(var(--card))",
                    foreground: "hsl(var(--card-foreground))",
                },
            },
            borderRadius: {
                lg: "var(--radius)",
                md: "calc(var(--radius) - 2px)",
                sm: "calc(var(--radius) - 4px)",
            },
            keyframes: {
                "spin-hex": {
                    "0%": { transform: "rotate(0deg)" },
                    "100%": { transform: "rotate(360deg)" },
                },
                "bounce-hex": {
                    "0%, 100%": { transform: "translateY(0)" },
                    "50%": { transform: "translateY(-5px)" },
                },
                "pulse-hex": {
                    "0%": { transform: "scale(1)", opacity: "1" },
                    "100%": { transform: "scale(1.5)", opacity: "0" },
                },
                "reveal-scale": {
                    "0%": { transform: "scale(0)", opacity: "0" },
                    "100%": { transform: "scale(1)", opacity: "1" },
                },
                "ripple-out": {
                    "0%": { transform: "scale(1)", opacity: "0.8", "strokeWidth": "2px" },
                    "100%": { transform: "scale(1.6)", opacity: "0", "strokeWidth": "0px" },
                },
                "shake-error": {
                    "0%, 100%": { transform: "translateX(0)" },
                    "20%": { transform: "translateX(-4px)" },
                    "40%": { transform: "translateX(4px)" },
                    "60%": { transform: "translateX(-2px)" },
                    "80%": { transform: "translateX(2px)" },
                },
                "morph-hex-square": {
                    "0%, 100%": { "clipPath": "polygon(50% 0%, 100% 25%, 100% 75%, 50% 100%, 0% 75%, 0% 25%)", "backgroundColor": "#E0FF00" },
                    "50%": { "clipPath": "polygon(0% 0%, 100% 0%, 100% 100%, 0% 100%, 0% 100%, 0% 0%)", "backgroundColor": "#FFFFFF" },
                },
                "dash-draw": {
                    "to": { "strokeDashoffset": "0" },
                }
            },
            animation: {
                "spin-slow": "spin-hex 3s linear infinite",
                "bounce-delay-1": "bounce-hex 1s infinite 0.1s",
                "bounce-delay-2": "bounce-hex 1s infinite 0.2s",
                "bounce-delay-3": "bounce-hex 1s infinite 0.3s",
                "pulse-hex": "pulse-hex 2s infinite",
                "reveal": "reveal-scale 0.5s cubic-bezier(0.34, 1.56, 0.64, 1) forwards",
                "shake": "shake-error 0.4s ease-in-out",
                "morph": "morph-hex-square 3s ease-in-out infinite",
                "dash-draw": "dash-draw 2s linear infinite"
            }
        },
    },
    plugins: [require("tailwindcss-animate")],
}
