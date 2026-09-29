var labels = document.documentElement.lang === 'ru'
  ? { copy: 'Копировать', done: 'Скопировано' }
  : { copy: 'Copy', done: 'Copied' };

document.querySelectorAll('.cmd').forEach(function (block) {
  if (!navigator.clipboard) return;
  var button = document.createElement('button');
  button.type = 'button';
  button.className = 'copy';
  button.textContent = labels.copy;
  button.addEventListener('click', function () {
    navigator.clipboard.writeText(block.querySelector('code').textContent).then(function () {
      button.textContent = labels.done;
      if (window.umami) window.umami.track('copy-command', { command: block.dataset.command });
      setTimeout(function () { button.textContent = labels.copy; }, 1600);
    });
  });
  block.appendChild(button);
});
