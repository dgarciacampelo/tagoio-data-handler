// tailwind.config.js
/** @type {import('tailwindcss').Config} */
module.exports = {
    darkMode: "class",
    // Tell Tailwind where to look for your HTML and JS files
    content: [
        "./templates/**/*.html",
        "./static/js/**/*.js"
    ],
    theme: {
        extend: {
            colors: {
                "background": "#f7f9fb",
                "on-background": "#191c1e",
                "primary": "#00571b",
                "on-primary": "#ffffff",
                "primary-fixed": "#9ef6b1",
                "on-primary-fixed": "#002106",
                "on-primary-fixed-variant": "#004e17",
                "secondary": "#006c49",
                "secondary-fixed-dim": "#4edea3",
                "error": "#ba1a1a",
                "error-container": "#ffdad6",
                "surface": "#f7f9fb",
                "on-surface": "#191c1e",
                "surface-variant": "#dee5df",
                "on-surface-variant": "#414942",
                "outline": "#717972",
                "outline-variant": "#c1c9c1",
                "surface-container-lowest": "#ffffff",
                "surface-container-low": "#f2f4f6",
                "surface-container": "#eceef0",
                "surface-container-highest": "#e0e3e5"
            },
            spacing: {
                "xs": "8px",
                "sm": "12px",
                "md": "16px",
                "lg": "24px",
                "xl": "32px",
                "grid-gutter": "16px",
                "base": "4px"
            },
            fontFamily: {
                "headline-md": ["Inter", "sans-serif"],
                "label-sm": ["Inter", "sans-serif"],
                "label-caps": ["Inter", "sans-serif"],
                "display-metrics": ["Inter", "sans-serif"],
                "body-md": ["Inter", "sans-serif"]
            },
            fontSize: {
                "headline-md": ["20px", { lineHeight: "28px", fontWeight: "600" }],
                "label-sm": ["12px", { lineHeight: "16px", fontWeight: "500" }],
                "label-caps": ["12px", { lineHeight: "16px", letterSpacing: "0.05em", fontWeight: "700", textTransform: "uppercase" }],
                "display-metrics": ["48px", { lineHeight: "56px", letterSpacing: "-0.02em", fontWeight: "700" }],
                "body-md": ["14px", { lineHeight: "20px", fontWeight: "400" }]
            }
        }
    }
}