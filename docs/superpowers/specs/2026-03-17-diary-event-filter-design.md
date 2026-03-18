# Diary Panel Event Type Filter — Design Spec

## Summary

Add event type filtering to the diary panel, alongside the existing agent filter. Users can toggle which event types (email, thinking, tool_call, etc.) are visible, reducing noise when monitoring agent activity.

## Motivation

The diary panel currently shows ALL event types for the selected agent (or all agents). With active agents producing thinking, tool_call, tool_result, reasoning, email_out, email_in, diary, and cancel events, the stream becomes unreadable. Users need to filter to e.g. "show only emails" or "hide tool results".

## Architecture

### Data Flow

```
DiaryPanel
  ├── state: activeTab (existing agent filter)
  ├── state: activeTypes: Set<DiaryEventType>  (all ON by default)
  ├── DiaryTabs
  │   ├── agent selector (existing, row 1)
  │   └── event type filter chips (NEW, row 2)
  └── entries.filter(byAgent).filter(byType) → DiaryEntry[]
```

### Files Changed

| File | Change |
|------|--------|
| `types.ts` | Add `ALL_DIARY_EVENT_TYPES` constant array |
| `DiaryTabs.tsx` | Change root layout to `flex flex-col`. Row 1: existing agent select. Row 2: new filter chip row. New props: `activeTypes`, `onToggleType`. |
| `DiaryPanel.tsx` | Add `activeTypes` state. Extend `useMemo` filter to include type filtering. Fix auto-scroll to only trigger on new entries (not filter changes). |

No backend changes. No new dependencies.

## UI Design

### Filter Chips

A horizontal row of small toggleable pills as a second row below the agent selector:

```
Diary  [All agents ▾]  :8001
[diary] [thinking] [tool] [why] [result] [sent] [received] [CANCELLED] [cancel diary] [event]
```

- Each chip uses the existing `TAG_COLORS[type]` for its `[background, foreground]` when active
- When inactive (filtered out): gray background (`#2a2a2a`), muted text (`#666`), `opacity: 0.3`
- Click to toggle individual type on/off
- All chips are ON by default
- Chips are small: `text-xs`, `px-2 py-0.5`, `rounded-full`, `cursor-pointer`
- Row uses `flex flex-wrap gap-1` — wraps on small viewports

### Labels

Use existing `TAG_LABELS` from `types.ts` exactly as defined:

| Type | `TAG_LABELS` value |
|------|-------------------|
| `diary` | `diary` |
| `thinking` | `thinking` |
| `tool_call` | `tool` |
| `reasoning` | `why` |
| `tool_result` | `result` |
| `email_out` | `sent` |
| `email_in` | `received` |
| `cancel_received` | `CANCELLED` |
| `cancel_diary` | `cancel diary` |
| `unknown` | `event` |

### Interaction

- Click a chip → toggle that type's visibility
- Visual feedback: active chips are colorful, inactive chips are gray/dim with reduced opacity

## Implementation Details

### types.ts

Add constant (all 10 event types):

```ts
export const ALL_DIARY_EVENT_TYPES: DiaryEventType[] = [
  "diary", "thinking", "tool_call", "reasoning",
  "tool_result", "email_out", "email_in",
  "cancel_received", "cancel_diary", "unknown",
];
```

### DiaryTabs.tsx

Root div changes from `flex items-center` to `flex flex-col`. The existing agent selector row becomes the first child. A new filter chip row becomes the second child.

New props added to `DiaryTabsProps`:

```ts
interface DiaryTabsProps {
  agents: AgentInfo[];
  activeTab: string;
  onTabChange: (tab: string) => void;
  activeTypes: Set<DiaryEventType>;           // NEW
  onToggleType: (type: DiaryEventType) => void; // NEW
}
```

Structure:

```tsx
<div className="flex flex-col border-b border-border">
  {/* Row 1: existing agent selector (unchanged content) */}
  <div className="flex items-center gap-2 px-3 py-1.5">
    <span className="text-[10px] text-text-dim uppercase tracking-wider">Diary</span>
    <select ...>...</select>
    {activeAgent && <span ...>:{activeAgent.port}</span>}
  </div>
  {/* Row 2: event type filter chips */}
  <div className="flex flex-wrap gap-1 px-3 pb-1.5">
    {ALL_DIARY_EVENT_TYPES.map(type => (
      <button
        key={type}
        onClick={() => onToggleType(type)}
        className={`text-xs px-2 py-0.5 rounded-full cursor-pointer transition-opacity ${
          activeTypes.has(type) ? '' : 'opacity-30'
        }`}
        style={{
          backgroundColor: activeTypes.has(type) ? TAG_COLORS[type][0] : '#2a2a2a',
          color: activeTypes.has(type) ? TAG_COLORS[type][1] : '#666',
        }}
      >
        {TAG_LABELS[type]}
      </button>
    ))}
  </div>
</div>
```

### DiaryPanel.tsx

```tsx
// New state
const [activeTypes, setActiveTypes] = useState<Set<DiaryEventType>>(
  new Set(ALL_DIARY_EVENT_TYPES)
);

const handleToggleType = (type: DiaryEventType) => {
  setActiveTypes(prev => {
    const next = new Set(prev);
    if (next.has(type)) next.delete(type);
    else next.add(type);
    return next;
  });
};

// Updated useMemo — filter by agent AND type
const filtered = useMemo(
  () =>
    entries
      .filter(e => activeTab === "all" || e.agent_key === activeTab)
      .filter(e => activeTypes.has(e.type)),
  [entries, activeTab, activeTypes]
);

// Auto-scroll: only on new entries, not on filter changes
const lastEntryTimeRef = useRef(0);
useEffect(() => {
  if (filtered.length === 0) return;
  const lastTime = filtered[filtered.length - 1].time;
  if (lastTime > lastEntryTimeRef.current) {
    lastEntryTimeRef.current = lastTime;
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }
}, [filtered]);
```

Pass new props to DiaryTabs:

```tsx
<DiaryTabs
  agents={agents}
  activeTab={activeTab}
  onTabChange={setActiveTab}
  activeTypes={activeTypes}
  onToggleType={handleToggleType}
/>
```

### Empty state

When all visible entries are filtered out, show a message:

```tsx
{filtered.length === 0 && (
  <div className="text-center text-text-dim text-xs py-4">
    No events match current filters
  </div>
)}
```

## Edge Cases

- **All types filtered out**: Show "No events match current filters" message
- **New event type added in future**: Add to `ALL_DIARY_EVENT_TYPES` in `types.ts`. Until added, events with that type won't have a filter chip but WILL be hidden if not in `activeTypes` — this is acceptable since adding a new type requires updating both `TAG_COLORS`/`TAG_LABELS` and `ALL_DIARY_EVENT_TYPES`.
- **Auto-scroll on filter change**: Does NOT auto-scroll when toggling filter chips. Only auto-scrolls when new entries arrive that are visible.
- **Performance**: `useMemo` with `[entries, activeTab, activeTypes]` deps ensures filtering only re-runs when inputs change.
