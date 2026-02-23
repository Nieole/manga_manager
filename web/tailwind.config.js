/** @type {import('tailwindcss').Config} */
export default {
    content: [
        "./index.html",
        "./src/**/*.{js,ts,jsx,tsx}",
    ],
    theme: {
        extend: {
            colors: {
                komgaDark: '#121212',
                komgaSurface: '#1E1E1E',
                komgaPrimary: '#BB86FC',
                komgaSecondary: '#03DAC6',
            }
        },
    },
    plugins: [],
}
