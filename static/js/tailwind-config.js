tailwind.config = {
    darkMode: "class",
    theme: {
        extend: {
            "colors": {
                "background": "#f7f9fb",
                "on-background": "#191c1e",
                "primary": "#00288e",
                "on-primary": "#ffffff",
                "primary-fixed": "#dde1ff",
                "on-primary-fixed": "#001453",
                "on-primary-fixed-variant": "#173bab",
                "secondary": "#006c49",
                "secondary-fixed-dim": "#4edea3",
                "error": "#ba1a1a",
                "error-container": "#ffdad6",
                "surface": "#f7f9fb",
                "on-surface": "#191c1e",
                "surface-variant": "#e0e3e5",
                "on-surface-variant": "#444653",
                "outline": "#757684",
                "outline-variant": "#c4c5d5",
                "surface-container-lowest": "#ffffff",
                "surface-container-low": "#f2f4f6",
                "surface-container": "#eceef0",
                "surface-container-highest": "#e0e3e5"
            },
            "spacing": {
                "xs": "8px",
                "sm": "12px",
                "md": "16px",
                "lg": "24px",
                "xl": "32px",
                "grid-gutter": "16px",
                "base": "4px"
            },
            "fontFamily": {
                "headline-md": ["Inter", "sans-serif"],
                "label-sm": ["Inter", "sans-serif"],
                "label-caps": ["Inter", "sans-serif"],
                "display-metrics": ["Inter", "sans-serif"],
                "body-md": ["Inter", "sans-serif"]
            },
            "fontSize": {
                "headline-md": ["20px", { "lineHeight": "28px", "fontWeight": "600" }],
                "label-sm": ["12px", { "lineHeight": "16px", "fontWeight": "500" }],
                "label-caps": ["12px", { "lineHeight": "16px", "letterSpacing": "0.05em", "fontWeight": "700", "textTransform": "uppercase" }],
                "display-metrics": ["48px", { "lineHeight": "56px", "letterSpacing": "-0.02em", "fontWeight": "700" }],
                "body-md": ["14px", { "lineHeight": "20px", "fontWeight": "400" }]
            }
        }
    }
}