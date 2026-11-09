const status = document.querySelector('#copy-status');
for (const pre of document.querySelectorAll('pre')) {
  const code = pre.querySelector('code');
  if (!code) continue;
  const button = document.createElement('button');
  button.type = 'button';
  button.className = 'copy-code';
  button.textContent = 'Copy';
  button.setAttribute('aria-label', 'Copy code block');
  button.addEventListener('click', async () => {
    try {
      await navigator.clipboard.writeText(code.textContent);
      button.textContent = 'Copied';
      status.textContent = 'Code copied to clipboard.';
    } catch {
      button.textContent = 'Select code';
      const range = document.createRange();
      range.selectNodeContents(code);
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      status.textContent = 'Clipboard unavailable. Code selected; copy it using your browser.';
    }
    window.setTimeout(() => { button.textContent = 'Copy'; }, 2000);
  });
  pre.append(button);
}
const navigation = document.querySelector('.docs-navigation');
// Keep navigation expanded without JavaScript; collapse it on small screens after load.
const narrow = window.matchMedia('(max-width: 900px)');
navigation.open = !narrow.matches;
narrow.addEventListener('change', event => { navigation.open = !event.matches; });
