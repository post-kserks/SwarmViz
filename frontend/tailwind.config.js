/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        bgWindow: '#0a0b0e',
        bgCard: '#12141c',
        borderCyber: '#1e2230',
        textPrimary: '#e2e8f0',
        textMuted: '#64748b',
        glowOrchestrator: '#a855f7',
        glowTeamwork: '#3b82f6',
        glowChallenger: '#f43f5e',
        glowWorker: '#f59e0b',
        diffAddBg: 'rgba(34, 197, 94, 0.15)',
        diffRemoveBg: 'rgba(239, 68, 68, 0.15)',
        conflictAlert: '#eab308',
      },
      fontFamily: {
        mono: ['JetBrains Mono', 'Fira Code', 'ui-monospace', 'monospace'],
        sans: ['Inter', 'system-ui', 'sans-serif'],
      },
      animation: {
        'pulse-glow-orchestrator': 'pulseGlowOrchestrator 2.5s infinite ease-in-out',
        'pulse-glow-teamwork': 'pulseGlowTeamwork 2.5s infinite ease-in-out',
        'pulse-glow-challenger': 'pulseGlowChallenger 2.5s infinite ease-in-out',
        'pulse-glow-worker': 'pulseGlowWorker 2.5s infinite ease-in-out',
        'dash-march': 'dashMarch 1s linear infinite',
      },
      keyframes: {
        pulseGlowOrchestrator: {
          '0%, 100%': { boxShadow: '0 0 8px rgba(168, 85, 247, 0.4)' },
          '50%': { boxShadow: '0 0 20px rgba(168, 85, 247, 0.9), 0 0 30px rgba(168, 85, 247, 0.5)' },
        },
        pulseGlowTeamwork: {
          '0%, 100%': { boxShadow: '0 0 8px rgba(59, 130, 246, 0.4)' },
          '50%': { boxShadow: '0 0 20px rgba(59, 130, 246, 0.9), 0 0 30px rgba(59, 130, 246, 0.5)' },
        },
        pulseGlowChallenger: {
          '0%, 100%': { boxShadow: '0 0 8px rgba(244, 63, 94, 0.4)' },
          '50%': { boxShadow: '0 0 20px rgba(244, 63, 94, 0.9), 0 0 30px rgba(244, 63, 94, 0.5)' },
        },
        pulseGlowWorker: {
          '0%, 100%': { boxShadow: '0 0 8px rgba(245, 158, 11, 0.4)' },
          '50%': { boxShadow: '0 0 20px rgba(245, 158, 11, 0.9), 0 0 30px rgba(245, 158, 11, 0.5)' },
        },
        dashMarch: {
          to: { strokeDashoffset: '-20' },
        },
      },
    },
  },
  plugins: [],
};
