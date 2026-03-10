self.addEventListener('install', (event) => {
    self.skipWaiting();
});

self.addEventListener('activate', (event) => {
    event.waitUntil(self.clients.claim());
});

self.addEventListener('notificationclick', (event) => {
    const notification = event.notification;
    notification.close();

    const action = event.action;
    const data = notification.data || {};

    const promiseChain = clients.matchAll({
        type: 'window',
        includeUncontrolled: true
    }).then((windowClients) => {
        let matchingClient = null;

        for (let i = 0; i < windowClients.length; i++) {
            const client = windowClients[i];
            // Just grab any open chat window if possible
            if (client.url && client.url.includes('/chat.html')) {
                matchingClient = client;
                break;
            }
        }
        if (!matchingClient && windowClients.length > 0) {
            matchingClient = windowClients[0];
        }

        if (!action) {
            // Normal click
            if (matchingClient) {
                return matchingClient.focus();
            } else {
                return clients.openWindow('/chat.html');
            }
        }

        // Action click (approve/reject/recheck)
        if (matchingClient) {
            // Tell the open window to handle the action
            matchingClient.postMessage({
                category: 'genie-action',
                action: action,
                approvalId: data.approvalId,
                type: data.type
            });
            return matchingClient.focus();
        } else {
            // If no window is open, we can't easily perform the API request because we don't have the auth tokens.
            // Best we can do is just open the window.
            return clients.openWindow('/chat.html');
        }
    });

    event.waitUntil(promiseChain);
});
