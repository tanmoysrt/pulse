// Per-agent capability tables.
export const AGENT_LABELS = { claude: 'Claude', codex: 'Codex', opencode: 'OpenCode' }
export const AGENT_ABBR = { claude: 'CC', codex: 'CX', opencode: 'OC' }
export const AGENTS = ['claude', 'codex', 'opencode']

export const MODES_BY_AGENT = {
  claude: [
    { id: 'manual', label: 'Manual' },
    { id: 'acceptEdits', label: 'Edit' },
    { id: 'plan', label: 'Plan' },
    { id: 'auto', label: 'Auto' },
  ],
  codex: [
    { id: 'default', label: 'Default' },
    { id: 'plan', label: 'Plan' },
  ],
  opencode: [
    { id: 'build', label: 'Build' },
    { id: 'plan', label: 'Plan' },
  ],
}

export const STATIC_MODELS_BY_AGENT = {
  claude: [
    { id: 'default', label: 'Default' },
    { id: 'opus', label: 'Opus' },
    { id: 'sonnet', label: 'Sonnet' },
    { id: 'haiku', label: 'Haiku' },
  ],
  codex: [
    { id: 'gpt-5.5', label: 'GPT-5.5' },
    { id: 'gpt-5.4', label: 'GPT-5.4' },
    { id: 'gpt-5.4-mini', label: 'GPT-5.4 Mini' },
  ],
}

export const EFFORTS_BY_AGENT = {
  claude: [
    { id: 'low', label: 'Low' },
    { id: 'medium', label: 'Medium' },
    { id: 'high', label: 'High' },
    { id: 'xhigh', label: 'XHigh' },
    { id: 'max', label: 'Max' },
  ],
  codex: [
    { id: 'low', label: 'Low' },
    { id: 'medium', label: 'Medium' },
    { id: 'high', label: 'High' },
    { id: 'xhigh', label: 'XHigh' },
  ],
}

export const WORKING_VERBS = [
  'Working', 'Thinking', 'Crunching', 'Cooking', 'Percolating',
  'Simmering', 'Churning', 'Pondering', 'Brewing', 'Conjuring',
]

export function agentLabel(agent) { return AGENT_LABELS[agent] || 'your session' }
