package fs

import (
	"fmt"
	"strings"
)

// NetworkOptions selects optional work while building a network snapshot.
type NetworkOptions struct {
	SkipMailEdges       bool
	AgentEntryLimit     int
	TopologyRecordLimit int
	ContactRecordLimit  int
	DaemonDispatchLimit int
	FixedFileByteLimit  int64
	JSONLByteLimit      int64
	ReadStats           *[]BoundedReadStats
}

// KanbanNetworkOptions is the complete bounded/manual-refresh profile. Keeping
// it here makes the SkipMailEdges and 1000-record guarantees reviewable at one
// call site while preserving zero-value, whole-source behavior for Portal and
// other existing callers.
func KanbanNetworkOptions(stats *[]BoundedReadStats) NetworkOptions {
	return NetworkOptions{
		SkipMailEdges:       true,
		AgentEntryLimit:     KanbanReadLimit,
		TopologyRecordLimit: KanbanReadLimit,
		ContactRecordLimit:  KanbanReadLimit,
		DaemonDispatchLimit: KanbanReadLimit,
		FixedFileByteLimit:  KanbanFixedFileByteLimit,
		JSONLByteLimit:      KanbanJSONLByteLimit,
		ReadStats:           stats,
	}
}

func BuildNetwork(baseDir string) (Network, error) {
	return BuildNetworkWithOptions(baseDir, NetworkOptions{})
}

func BuildNetworkWithOptions(baseDir string, opts NetworkOptions) (Network, error) {
	var nodes []AgentNode
	agentDirectoryComplete := false
	var err error
	if opts.AgentEntryLimit > 0 {
		nodes, agentDirectoryComplete, err = discoverKanbanAgents(baseDir, opts.AgentEntryLimit, opts.FixedFileByteLimit, opts.ReadStats)
	} else {
		nodes, err = DiscoverAgents(baseDir)
	}
	if err != nil {
		return Network{}, fmt.Errorf("discover agents: %w", err)
	}

	if opts.FixedFileByteLimit > 0 {
		normalizeKanbanAgentLiveness(nodes, opts.FixedFileByteLimit, opts.ReadStats)
	} else {
		normalizeAgentLiveness(nodes)
	}

	nodeIndex := make(map[string]bool)
	for _, n := range nodes {
		nodeIndex[n.WorkingDir] = true
	}

	var avatarEdges []AvatarEdge
	for _, n := range nodes {
		var edges []AvatarEdge
		var childDirs []string
		if opts.TopologyRecordLimit > 0 {
			edges, childDirs = readKanbanLedger(n.WorkingDir, baseDir, opts.TopologyRecordLimit, opts.JSONLByteLimit, opts.ReadStats)
		} else {
			edges, childDirs = ReadLedger(n.WorkingDir)
		}
		avatarEdges = append(avatarEdges, edges...)
		for _, cd := range childDirs {
			if opts.AgentEntryLimit > 0 && len(nodes) >= opts.AgentEntryLimit {
				break
			}
			if !nodeIndex[cd] {
				relCD := RelativizeAddress(cd, baseDir)
				nodes = append(nodes, AgentNode{
					Address:    relCD,
					AgentName:  "",
					WorkingDir: cd,
				})
				nodeIndex[cd] = true
			}
		}
	}

	var contactEdges []ContactEdge
	for _, n := range nodes {
		if opts.ContactRecordLimit > 0 {
			contactEdges = append(contactEdges, readKanbanContacts(n.WorkingDir, opts.ContactRecordLimit, opts.FixedFileByteLimit, opts.ReadStats)...)
		} else {
			contactEdges = append(contactEdges, ReadContacts(n.WorkingDir)...)
		}
	}

	// Count from inbox only — sent would double-count. Live snapshots can
	// skip this historical scan; BuildNetwork keeps the full default.
	var mailEdges []MailEdge
	if !opts.SkipMailEdges {
		mailEdges = buildMailEdges(nodes, baseDir)
	}
	stats := computeStats(nodes, mailEdges)
	activity := computeNetworkActivityWithOptions(nodes, opts)

	// Relativize all edge addresses so they match AgentNode.Address format
	for i := range avatarEdges {
		avatarEdges[i].Parent = RelativizeAddress(avatarEdges[i].Parent, baseDir)
		avatarEdges[i].Child = RelativizeAddress(avatarEdges[i].Child, baseDir)
	}
	for i := range contactEdges {
		contactEdges[i].Owner = RelativizeAddress(contactEdges[i].Owner, baseDir)
		contactEdges[i].Target = RelativizeAddress(contactEdges[i].Target, baseDir)
	}

	return Network{
		Nodes:                   nodes,
		AvatarEdges:             avatarEdges,
		ContactEdges:            contactEdges,
		MailEdges:               mailEdges,
		Stats:                   stats,
		Activity:                activity,
		AgentDirectoryTruncated: opts.AgentEntryLimit > 0 && !agentDirectoryComplete,
	}, nil
}

func buildMailEdges(nodes []AgentNode, baseDir string) []MailEdge {
	type edgeKey struct{ sender, recipient string }
	counts := make(map[edgeKey]int)

	for _, n := range nodes {
		if n.WorkingDir == "" {
			continue
		}
		inbox, _ := ReadInbox(n.WorkingDir)
		for _, msg := range inbox {
			from := RelativizeAddress(ResolveAddress(msg.From, baseDir), baseDir)
			recipients := NormalizeMailEndpoints(msg.To)
			for _, r := range recipients {
				counts[edgeKey{from, RelativizeAddress(ResolveAddress(r, baseDir), baseDir)}]++
			}
		}
	}

	var edges []MailEdge
	for k, c := range counts {
		edges = append(edges, MailEdge{
			Sender:    k.sender,
			Recipient: k.recipient,
			Count:     c,
		})
	}
	return edges
}

func computeStats(nodes []AgentNode, mailEdges []MailEdge) NetworkStats {
	var s NetworkStats
	for _, n := range nodes {
		switch strings.ToUpper(n.State) {
		case "ACTIVE":
			s.Active++
		case "IDLE":
			s.Idle++
		case "STUCK":
			s.Stuck++
		case "ASLEEP":
			s.Asleep++
		case "SUSPENDED":
			s.Suspended++
		}
	}
	for _, e := range mailEdges {
		s.TotalMails += e.Count
	}
	return s
}
