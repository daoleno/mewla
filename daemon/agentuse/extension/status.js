const state = document.getElementById('state');
const detail = document.getElementById('detail');
chrome.runtime.sendMessage({ type: 'status' }).then(status => {
  if (!status?.connected) {
    state.textContent = 'Waiting for Mewla';
    state.className = 'state warn';
    detail.textContent = "Mewla isn't running on this computer, or isn't installed. Agents can't use this Chrome until it is.";
    return;
  }
  state.className = 'state ok';
  if (status.sessions > 0) {
    state.textContent = status.sessions === 1 ? 'An agent is working' : `${status.sessions} agents are working`;
    detail.textContent = 'They are in their own window. Close it to stop them.';
  } else {
    state.textContent = 'Connected to Mewla';
    detail.textContent = 'No agent is using Chrome now.';
  }
}, () => {
  state.textContent = 'Not available';
  state.className = 'state warn';
});
