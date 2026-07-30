function networkWithMailEdges(current, mailNetwork) {
  if (!current) return mailNetwork;
  return {
    ...current,
    mail_edges: mailNetwork.mail_edges,
    stats: {
      ...current.stats,
      total_mails: mailNetwork.stats.total_mails,
    },
  };
}

export function createLiveNetworkCoordinator({
  fetchNetwork,
  onFastNetwork,
  onFullNetwork,
  onMailAvailability,
  schedule = (fn, ms) => setInterval(fn, ms),
  cancelSchedule = (id) => clearInterval(id),
  intervalMs = 3000,
}) {
  let active = false;
  let generation = 0;
  let mode = 'avatar';
  let scheduleID = null;
  let fastController = null;
  let fullController = null;
  let latestFastNetwork = null;
  let loadedMailNetwork = null;

  const current = (serial, controller, requireEmail = false) => (
    active && serial === generation &&
    (controller === fastController || controller === fullController) &&
    (!requireEmail || mode === 'email')
  );

  const emitFast = (network, requireEmail = false, serial = generation, controller = fastController) => {
    if (!current(serial, controller, requireEmail)) return;
    latestFastNetwork = network;
    const merged = mode === 'email' && loadedMailNetwork
      ? networkWithMailEdges(network, loadedMailNetwork)
      : network;
    onFastNetwork(merged);
  };

  const poll = (serial) => {
    if (!active || serial !== generation) return;

    if (!fastController) {
      const controller = new AbortController();
      fastController = controller;
      // The live fast lane must opt into the incomplete mail=0 contract.
      fetchNetwork({ includeMailEdges: false, signal: controller.signal })
        .then((network) => emitFast(network, false, serial, controller))
        .catch(() => {})
        .finally(() => {
          if (fastController === controller) fastController = null;
        });
    }

    if (mode !== 'email' || fullController) return;

    // A new full request invalidates the prior mail projection. Until this
    // request completes, the UI must not present the prior mail history as
    // current. The fast node/status lane continues independently.
    loadedMailNetwork = null;
    onMailAvailability(false);
    if (latestFastNetwork) onFastNetwork(latestFastNetwork);

    const controller = new AbortController();
    fullController = controller;
    fetchNetwork({ includeMailEdges: true, signal: controller.signal })
      .then((network) => {
        if (!current(serial, controller, true)) return;
        loadedMailNetwork = network;
        const merged = networkWithMailEdges(latestFastNetwork, network);
        onMailAvailability(true);
        onFullNetwork(merged);
      })
      .catch(() => {})
      .finally(() => {
        if (fullController === controller) fullController = null;
      });
  };

  const stop = () => {
    active = false;
    generation += 1;
    if (scheduleID !== null) cancelSchedule(scheduleID);
    scheduleID = null;
    if (fastController) fastController.abort();
    if (fullController) fullController.abort();
    fastController = null;
    fullController = null;
    latestFastNetwork = null;
    loadedMailNetwork = null;
  };

  return {
    start(nextMode = 'avatar') {
      stop();
      active = true;
      mode = nextMode;
      onMailAvailability(false);
      const serial = generation;
      poll(serial);
      scheduleID = schedule(() => poll(serial), intervalMs);
    },
    stop,
  };
}
