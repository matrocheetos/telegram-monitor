document.addEventListener('DOMContentLoaded', () => {
  // Copy to clipboard buttons
  document.querySelectorAll('[data-copy-target]').forEach(button => {
    button.addEventListener('click', () => {
      const targetId = button.getAttribute('data-copy-target');
      const input = document.getElementById(targetId);
      if (!input) return;

      const text = input.value || input.innerText;
      navigator.clipboard.writeText(text).then(() => {
        const originalText = button.innerText;
        button.innerText = button.getAttribute('data-copied-label') || 'Copiado!';
        button.classList.add('btn-success');
        setTimeout(() => {
          button.innerText = originalText;
          button.classList.remove('btn-success');
        }, 2000);
      });
    });
  });

  // Table search filter (Channels table)
  const searchInput = document.getElementById('channelSearchInput');
  if (searchInput) {
    searchInput.addEventListener('input', () => {
      const filter = searchInput.value.toLowerCase().trim();
      const rows = document.querySelectorAll('#channelsTable tbody tr');
      rows.forEach(row => {
        const text = row.innerText.toLowerCase();
        if (text.includes(filter)) {
          row.style.display = '';
        } else {
          row.style.display = 'none';
        }
      });
    });
  }

  // Password Reset Modal handlers
  document.querySelectorAll('[data-open-modal]').forEach(btn => {
    btn.addEventListener('click', () => {
      const modalId = btn.getAttribute('data-open-modal');
      const modal = document.getElementById(modalId);
      if (modal) {
        const userId = btn.getAttribute('data-user-id');
        const username = btn.getAttribute('data-username');
        const form = modal.querySelector('form');
        if (form && userId) {
          form.action = `/admin/users/${userId}/password`;
        }
        const userPlaceholder = modal.querySelector('.modal-username');
        if (userPlaceholder && username) {
          userPlaceholder.innerText = `@${username}`;
        }
        modal.classList.add('open');
      }
    });
  });

  document.querySelectorAll('[data-close-modal]').forEach(btn => {
    btn.addEventListener('click', () => {
      const modal = btn.closest('.modal');
      if (modal) modal.classList.remove('open');
    });
  });
});
