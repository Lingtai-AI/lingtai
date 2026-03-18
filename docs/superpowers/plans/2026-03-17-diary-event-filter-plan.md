# Implementation Plan: Diary Panel Event Type Filter

**Spec:** `docs/superpowers/specs/2026-03-17-diary-event-filter-design.md`

## Prerequisites

Read the spec file first. All design decisions are documented there.

## Steps

### Step 1: Add `ALL_DIARY_EVENT_TYPES` to `types.ts`

Add the constant array after the existing `TAG_LABELS`:

```ts
export const ALL_DIARY_EVENT_TYPES: DiaryEventType[] = [
  "diary", "thinking", "tool_call", "reasoning",
  "tool_result", "email_out", "email_in",
  "cancel_received", "cancel_diary", "unknown",
];
```

**Smoke test**: `python -c "..."` doesn't apply here — run `npm run dev` to verify no compile errors.

### Step 2: Update `DiaryTabs.tsx` — Add filter chip row

1. Add new props to `DiaryTabsProps`:
   ```ts
   activeTypes: Set<DiaryEventType>;
   onToggleType: (type: DiaryEventType) => void;
   ```

2. Add imports: `DiaryEventType`, `ALL_DIARY_EVENT_TYPES`, `TAG_COLORS`, `TAG_LABELS` from `../types`

3. Change root div from `flex items-center` to `flex flex-col`. The existing agent selector row becomes the first child div (`flex items-center gap-2 px-3 py-1.5`). Add a second child div for the filter chips (`flex flex-wrap gap-1 px-3 pb-1.5`).

4. Move the `border-b border-border` from the inner row to the root `flex flex-col` div.

5. Render filter chips:
   ```tsx
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
   ```

**Verify**: Chips render below the agent selector. Clicking toggles visual state (even though filtering isn't connected yet).

### Step 3: Update `DiaryPanel.tsx` — Add type filter state and logic

1. Add imports: `DiaryEventType`, `ALL_DIARY_EVENT_TYPES` from `../types`

2. Add state:
   ```ts
   const [activeTypes, setActiveTypes] = useState<Set<DiaryEventType>>(
     new Set(ALL_DIARY_EVENT_TYPES)
   );
   ```

3. Add toggle handler:
   ```ts
   const handleToggleType = (type: DiaryEventType) => {
     setActiveTypes(prev => {
       const next = new Set(prev);
       if (next.has(type)) next.delete(type);
       else next.add(type);
       return next;
     });
   };
   ```

4. Update the `useMemo` filter to include type filtering:
   ```ts
   const filtered = useMemo(
     () =>
       entries
         .filter(e => activeTab === "all" || e.agent_key === activeTab)
         .filter(e => activeTypes.has(e.type)),
     [entries, activeTab, activeTypes]
   );
   ```

5. Pass new props to `DiaryTabs`:
   ```tsx
   <DiaryTabs
     agents={agents}
     activeTab={activeTab}
     onTabChange={setActiveTab}
     activeTypes={activeTypes}
     onToggleType={handleToggleType}
   />
   ```

**Verify**: Clicking a chip now filters diary entries. Toggling "sent" hides email_out entries. Toggling "thinking" hides thinking entries.

### Step 4: Fix auto-scroll to not trigger on filter changes

Replace the current auto-scroll `useEffect` to only scroll when NEW entries arrive, not when filters change:

```ts
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

**Verify**: Toggling a filter chip does NOT scroll to bottom. New diary entries arriving DOES scroll to bottom.

### Step 5: Add empty state message

After the `filtered.map(...)` in DiaryPanel, add:

```tsx
{filtered.length === 0 && (
  <div className="text-center text-text-dim text-xs py-4">
    No events match current filters
  </div>
)}
```

**Verify**: Filter out all types → "No events match current filters" appears. Re-enable a type → entries reappear.

### Step 6: Final verification

1. Test with multiple agents active — filter by agent + type combo works
2. Test with "All agents" selected — type filter applies across all agents
3. Test empty diary (no entries yet) — filter chips visible, no errors
4. Visual check: chips look good in both dark and light mode
5. `npm run dev` compiles cleanly with no warnings
