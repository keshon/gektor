const tabs = [...document.querySelectorAll('[role="tab"]')];
let platform = 'windows';
function selectPlatform(next, focus = false) {
  platform = next;
  for (const tab of tabs) {
    const selected = tab.dataset.platform === next;
    tab.setAttribute('aria-selected', String(selected));
    tab.tabIndex = selected ? 0 : -1;
    document.querySelector(`#${tab.getAttribute('aria-controls')}`).hidden = !selected;
    if (selected && focus) tab.focus();
  }
  document.querySelector('#copy-status').textContent = '';
}
tabs.forEach((tab, index) => {
  tab.addEventListener('click', () => selectPlatform(tab.dataset.platform));
  tab.addEventListener('keydown', event => {
    let target;
    if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') target = tabs[(index + 1) % tabs.length];
    if (event.key === 'Home') target = tabs[0];
    if (event.key === 'End') target = tabs.at(-1);
    if (!target) return;
    event.preventDefault();
    selectPlatform(target.dataset.platform, true);
  });
});

document.querySelector('#copy-command').addEventListener('click', async () => {
  const command = document.querySelector(`#launch-${platform} code`).textContent;
  const status = document.querySelector('#copy-status');
  try {
    await navigator.clipboard.writeText(command);
    status.textContent = 'Command copied.';
  } catch {
    status.textContent = 'Could not copy. Select the command and copy it manually.';
  }
});
