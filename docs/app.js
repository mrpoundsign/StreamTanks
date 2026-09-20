/**
 * StreamTanks GitHub Pages Documentation — Interactive Scripts
 */

document.addEventListener('DOMContentLoaded', () => {
  // 1. Mobile Menu Toggle
  const mobileToggle = document.getElementById('mobileToggle');
  const navMenu = document.getElementById('navMenu');

  if (mobileToggle && navMenu) {
    mobileToggle.addEventListener('click', () => {
      navMenu.classList.toggle('open');
      mobileToggle.classList.toggle('open');
    });

    // Close menu when clicking nav links
    navMenu.querySelectorAll('.nav-link').forEach(link => {
      link.addEventListener('click', () => {
        navMenu.classList.remove('open');
        mobileToggle.classList.remove('open');
      });
    });
  }

  // 2. Hero Visual Showcase Tabs
  const heroTabs = document.querySelectorAll('#heroTabs .window-tab');
  const heroImage = document.getElementById('heroImage');
  const heroImageDesc = document.getElementById('heroImageDesc');

  if (heroTabs.length > 0 && heroImage) {
    heroTabs.forEach(tab => {
      tab.addEventListener('click', () => {
        heroTabs.forEach(t => t.classList.remove('active'));
        tab.classList.add('active');

        const newSrc = tab.getAttribute('data-img');
        const newDesc = tab.getAttribute('data-desc');

        heroImage.style.opacity = '0';
        setTimeout(() => {
          heroImage.src = newSrc;
          if (heroImageDesc) heroImageDesc.textContent = newDesc;
          heroImage.style.opacity = '1';
        }, 150);
      });
    });
  }

  // 3. Screenshot Gallery Lightbox
  const lightboxModal = document.getElementById('lightboxModal');
  const lightboxImg = document.getElementById('lightboxImg');
  const lightboxTitle = document.getElementById('lightboxTitle');
  const lightboxSubtitle = document.getElementById('lightboxSubtitle');
  const lightboxClose = document.getElementById('lightboxClose');
  const lightboxBackdrop = document.getElementById('lightboxBackdrop');
  const galleryItems = document.querySelectorAll('.gallery-item');

  function openLightbox(imgSrc, title, subtitle) {
    if (!lightboxModal) return;
    lightboxImg.src = imgSrc;
    lightboxTitle.textContent = title;
    lightboxSubtitle.textContent = subtitle;
    lightboxModal.classList.add('open');
    lightboxModal.setAttribute('aria-hidden', 'false');
    document.body.style.overflow = 'hidden';
  }

  function closeLightbox() {
    if (!lightboxModal) return;
    lightboxModal.classList.remove('open');
    lightboxModal.setAttribute('aria-hidden', 'true');
    document.body.style.overflow = '';
  }

  galleryItems.forEach(item => {
    item.addEventListener('click', () => {
      const fullImg = item.getAttribute('data-full');
      const title = item.getAttribute('data-title') || '';
      const subtitle = item.getAttribute('data-subtitle') || '';
      openLightbox(fullImg, title, subtitle);
    });
  });

  if (lightboxClose) lightboxClose.addEventListener('click', closeLightbox);
  if (lightboxBackdrop) lightboxBackdrop.addEventListener('click', closeLightbox);

  window.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && lightboxModal && lightboxModal.classList.contains('open')) {
      closeLightbox();
    }
  });

  // 4. Click-to-Copy for Code Snippets & Chat Commands
  function handleCopy(button, textToCopy) {
    if (!navigator.clipboard) {
      // Fallback for non-secure contexts
      const tempInput = document.createElement('textarea');
      tempInput.value = textToCopy;
      document.body.appendChild(tempInput);
      tempInput.select();
      document.execCommand('copy');
      document.body.removeChild(tempInput);
    } else {
      navigator.clipboard.writeText(textToCopy);
    }

    const originalText = button.textContent;
    button.textContent = '✓ Copied!';
    button.classList.add('copied');

    setTimeout(() => {
      button.textContent = originalText;
      button.classList.remove('copied');
    }, 1800);
  }

  // Snippet box copy buttons
  document.querySelectorAll('.copy-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      const text = btn.getAttribute('data-copy');
      if (text) handleCopy(btn, text);
    });
  });

  // Table row copy buttons
  document.querySelectorAll('.btn-copy-cmd').forEach(btn => {
    btn.addEventListener('click', () => {
      const cmd = btn.getAttribute('data-cmd');
      if (cmd) handleCopy(btn, cmd);
    });
  });

  // 5. Chat Command Table Filter & Search
  const filterBtns = document.querySelectorAll('#commandFilters .filter-btn');
  const searchInput = document.getElementById('commandSearch');
  const tableRows = document.querySelectorAll('#commandTable tbody tr');

  let activeFilter = 'all';
  let searchQuery = '';

  function updateTableVisibility() {
    tableRows.forEach(row => {
      const category = row.getAttribute('data-category');
      const text = row.textContent.toLowerCase();

      const matchesFilter = (activeFilter === 'all') || (category === activeFilter);
      const matchesSearch = !searchQuery || text.includes(searchQuery);

      if (matchesFilter && matchesSearch) {
        row.style.display = '';
      } else {
        row.style.display = 'none';
      }
    });
  }

  filterBtns.forEach(btn => {
    btn.addEventListener('click', () => {
      filterBtns.forEach(b => b.classList.remove('active'));
      btn.classList.add('active');
      activeFilter = btn.getAttribute('data-filter');
      updateTableVisibility();
    });
  });

  if (searchInput) {
    searchInput.addEventListener('input', (e) => {
      searchQuery = e.target.value.toLowerCase().trim();
      updateTableVisibility();
    });
  }

  // 6. Active Nav Link Tracking on Scroll
  const navLinks = document.querySelectorAll('.nav-link');
  const sections = document.querySelectorAll('section[id]');

  if (window.IntersectionObserver && sections.length > 0) {
    const observer = new IntersectionObserver((entries) => {
      entries.forEach(entry => {
        if (entry.isIntersecting) {
          const id = entry.target.getAttribute('id');
          navLinks.forEach(link => {
            if (link.getAttribute('href') === `#${id}`) {
              link.classList.add('active');
            } else {
              link.classList.remove('active');
            }
          });
        }
      });
    }, { rootMargin: '-20% 0px -70% 0px' });

    sections.forEach(sec => observer.observe(sec));
  }
});
