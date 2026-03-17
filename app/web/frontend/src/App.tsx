import { useAgents } from "./hooks/useAgents";
import { useInbox } from "./hooks/useInbox";
import { useDiary } from "./hooks/useDiary";
import { Header } from "./components/Header";
import { InboxPanel } from "./components/InboxPanel";
import { DiaryPanel } from "./components/DiaryPanel";

const USER_PORT = 8300;

export default function App() {
  const { agents, keyToName, addressToName } = useAgents();
  const { receivedEmails, sentMessages, addSent } = useInbox();
  const entries = useDiary(agents);

  return (
    <div className="h-screen flex flex-col bg-bg text-text font-sans">
      <Header agents={agents} userPort={USER_PORT} />
      <div className="flex-1 flex overflow-hidden">
        <InboxPanel
          agents={agents}
          keyToName={keyToName}
          addressToName={addressToName}
          receivedEmails={receivedEmails}
          sentMessages={sentMessages}
          onSent={addSent}
        />
        <DiaryPanel
          agents={agents}
          entries={entries}
          addressToName={addressToName}
        />
      </div>
    </div>
  );
}
