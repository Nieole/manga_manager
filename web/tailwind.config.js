const colorVar = (name) => `rgb(var(${name}) / <alpha-value>)`;

/** @type {import('tailwindcss').Config} */
export default {
    content: [
        "./index.html",
        "./src/**/*.{js,ts,jsx,tsx}",
    ],
    darkMode: 'class',
    theme: {
        extend: {
            colors: {
                black: colorVar('--color-black'),
                white: colorVar('--color-white'),
                gray: {
                    50: colorVar('--color-gray-50'),
                    100: colorVar('--color-gray-100'),
                    200: colorVar('--color-gray-200'),
                    300: colorVar('--color-gray-300'),
                    400: colorVar('--color-gray-400'),
                    500: colorVar('--color-gray-500'),
                    600: colorVar('--color-gray-600'),
                    700: colorVar('--color-gray-700'),
                    800: colorVar('--color-gray-800'),
                    900: colorVar('--color-gray-900'),
                    950: colorVar('--color-gray-950'),
                },
                slate: {
                    50: colorVar('--color-gray-50'),
                    100: colorVar('--color-gray-100'),
                    200: colorVar('--color-gray-200'),
                    300: colorVar('--color-gray-300'),
                    400: colorVar('--color-gray-400'),
                    500: colorVar('--color-gray-500'),
                    600: colorVar('--color-gray-600'),
                    700: colorVar('--color-gray-700'),
                    800: colorVar('--color-gray-800'),
                    900: colorVar('--color-gray-900'),
                    950: colorVar('--color-gray-950'),
                },
                komgaDark: colorVar('--color-komga-dark'),
                komgaSurface: colorVar('--color-komga-surface'),
                komgaBackground: colorVar('--color-komga-background'),
                komgaSidebar: colorVar('--color-komga-sidebar'),
                komgaPrimary: colorVar('--color-komga-primary'),
                komgaPrimaryHover: colorVar('--color-komga-primary-hover'),
                komgaSecondary: colorVar('--color-komga-secondary'),
            }
        },
    },
    plugins: [],
}
