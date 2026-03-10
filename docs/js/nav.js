/**
 * Shared Navigation & Footer Component
 * Injects consistent header and footer across all pages.
 */
(function () {
  'use strict';

  const currentPage = window.location.pathname.split('/').pop() || 'index.html';

  function isActive(page) {
    if (page === 'index.html' && (currentPage === '' || currentPage === 'index.html')) return true;
    return currentPage === page;
  }

  function navLink(href, label) {
    const active = isActive(href);
    return `<a href="${href}" class="nav-link ${active ? 'active' : ''}">${label}</a>`;
  }

  function injectNav() {
    const navTarget = document.getElementById('nav-container');
    if (!navTarget) return;

    navTarget.innerHTML = `
      <nav class="nav-root fixed top-0 left-0 right-0 z-50 bg-white/95 backdrop-blur-lg border-b border-gray-100 safe-area-pad">
        <div class="nav-bar max-w-6xl mx-auto px-4 sm:px-6 h-14 sm:h-16 flex items-center justify-between">
          <!-- Logo -->
          <a href="index.html" class="nav-logo flex items-center gap-2 text-lg sm:text-xl font-bold text-gray-900 no-underline shrink-0">
            <span class="text-xl sm:text-2xl">🧞</span>
            <span>genie</span>
            <span class="nav-logo-by text-xs font-normal text-gray-400 ml-1">by Stackgen</span>
          </a>

          <!-- Desktop Nav (hidden on mobile via CSS - works even when injected after Tailwind scan) -->
          <div class="nav-desktop-links items-center gap-6 lg:gap-8" style="display: none;">
            ${navLink('index.html', 'Home')}
            ${navLink('docs.html', 'Docs')}
            ${navLink('ai-capabilities.html', 'AI Capabilities')}
            ${navLink('config-builder.html', 'Config Builder')}
            ${navLink('chat.html', 'Chat')}
            ${navLink('contact.html', 'Contact')}
          </div>

          <!-- Desktop CTA (hidden on mobile) -->
          <div class="nav-desktop-cta items-center gap-3" style="display: none;">
            <a href="https://github.com/stackgenhq/genie" target="_blank" rel="noopener"
               class="btn-secondary text-sm no-underline inline-flex items-center gap-2">
              <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 24 24"><path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/></svg>
              GitHub
            </a>
            <a href="https://stackgen.com" target="_blank" rel="noopener" class="btn-primary text-sm no-underline">
              Try Stackgen
            </a>
          </div>

          <!-- Mobile: Hamburger (shown only on small screens via CSS) -->
          <button type="button" id="mobile-menu-btn" class="nav-mobile-btn" style="display: none;" aria-label="Open menu" aria-expanded="false">
            <svg class="nav-mobile-btn-icon-open w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16"/>
            </svg>
            <svg class="nav-mobile-btn-icon-close w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true" style="display: none;">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"/>
            </svg>
          </button>
        </div>

        <!-- Mobile menu panel (slide-down, full-width links; visibility controlled by CSS + JS) -->
        <div id="mobile-menu" class="nav-mobile-menu" role="dialog" aria-label="Site menu" aria-modal="true" aria-hidden="true">
          <div class="nav-mobile-menu-inner">
            <div class="nav-mobile-links">
              ${navLink('index.html', 'Home')}
              ${navLink('docs.html', 'Docs')}
              ${navLink('ai-capabilities.html', 'AI Capabilities')}
              ${navLink('config-builder.html', 'Config Builder')}
              ${navLink('chat.html', 'Chat')}
              ${navLink('contact.html', 'Contact')}
            </div>
            <div class="nav-mobile-cta">
              <a href="https://github.com/stackgenhq/genie" target="_blank" rel="noopener" class="nav-mobile-cta-link nav-mobile-cta-secondary">GitHub</a>
              <a href="https://stackgen.com" target="_blank" rel="noopener" class="nav-mobile-cta-link nav-mobile-cta-primary">Try Stackgen</a>
            </div>
          </div>
        </div>
      </nav>
    `;

    // Mobile menu toggle and accessibility
    const menuBtn = document.getElementById('mobile-menu-btn');
    const menu = document.getElementById('mobile-menu');
    const iconOpen = menuBtn && menuBtn.querySelector('.nav-mobile-btn-icon-open');
    const iconClose = menuBtn && menuBtn.querySelector('.nav-mobile-btn-icon-close');

    function openMenu() {
      if (!menu || !menuBtn) return;
      menu.classList.add('nav-mobile-menu-open');
      menu.setAttribute('aria-hidden', 'false');
      menuBtn.setAttribute('aria-expanded', 'true');
      menuBtn.setAttribute('aria-label', 'Close menu');
      if (iconOpen) iconOpen.style.display = 'none';
      if (iconClose) iconClose.style.display = '';
      document.body.classList.add('nav-menu-open');
    }

    function closeMenu() {
      if (!menu || !menuBtn) return;
      menu.classList.remove('nav-mobile-menu-open');
      menu.setAttribute('aria-hidden', 'true');
      menuBtn.setAttribute('aria-expanded', 'false');
      menuBtn.setAttribute('aria-label', 'Open menu');
      if (iconOpen) iconOpen.style.display = '';
      if (iconClose) iconClose.style.display = 'none';
      document.body.classList.remove('nav-menu-open');
    }

    if (menuBtn && menu) {
      menuBtn.addEventListener('click', function () {
        if (menu.classList.contains('nav-mobile-menu-open')) {
          closeMenu();
        } else {
          openMenu();
        }
      });
      // Close when clicking a link (navigate away or same-page)
      menu.querySelectorAll('a').forEach(function (a) {
        a.addEventListener('click', closeMenu);
      });
    }
  }

  function injectFooter() {
    const footerTarget = document.getElementById('footer-container');
    if (!footerTarget) return;

    footerTarget.innerHTML = `
      <footer class="border-t border-gray-100 bg-white">
        <div class="max-w-6xl mx-auto px-6 py-12">
          <div class="grid grid-cols-1 md:grid-cols-4 gap-8">
            <!-- Brand -->
            <div class="md:col-span-2">
              <div class="flex items-center gap-2 text-lg font-bold text-gray-900 mb-3">
                <span class="text-xl">🧞</span>
                <span>genie</span>
              </div>
              <p class="text-gray-500 text-sm leading-relaxed max-w-md">
                An enterprise-grade agentic CLI powered by Stackgen.
                Multi-model, multi-tool, extensible — the agent that actually delivers.
              </p>
            </div>

            <!-- Links -->
            <div>
              <h4 class="text-sm font-semibold text-gray-900 mb-3">Product</h4>
              <div class="space-y-2">
                <a href="index.html" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">Home</a>
                <a href="docs.html" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">Docs</a>
                <a href="ai-capabilities.html" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">AI Capabilities</a>
                <a href="config-builder.html" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">Config Builder</a>
                <a href="chat.html" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">Chat</a>
                <a href="https://stackgen.com" target="_blank" rel="noopener" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">Stackgen Platform</a>
              </div>
            </div>

            <div>
              <h4 class="text-sm font-semibold text-gray-900 mb-3">Community</h4>
              <div class="space-y-2">
                <a href="https://github.com/stackgenhq/genie" target="_blank" rel="noopener" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">GitHub</a>
                <a href="https://github.com/stackgenhq/genie/blob/main/LICENSING.md" target="_blank" rel="noopener" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">License</a>
                <a href="https://github.com/stackgenhq/genie/issues" target="_blank" rel="noopener" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">Issues</a>
                <a href="contact.html" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">Contact Us</a>
                <a href="https://forms.gle/iF5fi1cssLB7uwAs6" target="_blank" rel="noopener" class="block text-sm text-gray-500 hover:text-purple-600 no-underline">Feedback</a>
              </div>
            </div>
          </div>

          <div class="border-t border-gray-100 mt-8 pt-6 flex flex-col md:flex-row items-center justify-between gap-4">
            <p class="text-xs text-gray-400">
              Built with ✨ by <a href="https://stackgen.com" target="_blank" rel="noopener" class="text-purple-500 hover:text-purple-600 no-underline">Stackgen</a>.
              Infrastructure is hard. Being a Genie is easy.
            </p>
            <p class="text-xs text-gray-400">© ${new Date().getFullYear()} Stackgen. All rights reserved.</p>
          </div>
        </div>
      </footer>
    `;
  }

  // Run on DOM ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => { injectNav(); injectFooter(); });
  } else {
    injectNav();
    injectFooter();
  }
})();
