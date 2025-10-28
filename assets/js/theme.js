// Global theme manager
window.Distribyted = window.Distribyted || {};
Distribyted.theme = (function(){
  var THEME_KEY = 'distribyted_theme';
  var COOKIE_NAME = 'distribyted_theme';

  function readCookie(name){
    try{
      var m = document.cookie.match(new RegExp('(?:^|; )'+name.replace(/([.$?*|{}()\[\]\\\/\+^])/g,'\\$1')+'=([^;]*)'));
      return m ? decodeURIComponent(m[1]) : null;
    }catch(e){ return null; }
  }

  function writeCookie(name, value, days){
    try{
      var d = new Date();
      d.setTime(d.getTime() + (days*24*60*60*1000));
      document.cookie = name + '=' + encodeURIComponent(value) + '; expires=' + d.toUTCString() + '; path=/';
    }catch(e){}
  }

  function getStoredTheme(){
    try{
      var t = localStorage.getItem(THEME_KEY) || readCookie(COOKIE_NAME);
      if(t === 'dark' || t === 'light') return t;
    }catch(e){}
    try{
      if(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches){ return 'dark'; }
    }catch(e){}
    return 'light';
  }

  function applyTheme(theme){
    var isDark = theme === 'dark';
    var html = document.documentElement;
    if(isDark){ html.classList.add('theme-dark'); html.setAttribute('data-theme','dark'); }
    else{ html.classList.remove('theme-dark'); html.setAttribute('data-theme','light'); }
    // If body exists, also toggle header class
    var body = document.body;
    if(body){
      try{
        body.classList.remove(isDark ? 'header-light' : 'header-dark');
        body.classList.add(isDark ? 'header-dark' : 'header-light');
      }catch(e){}
    }
  }

  function setTheme(theme){
    if(theme !== 'dark' && theme !== 'light') return;
    try{ localStorage.setItem(THEME_KEY, theme); }catch(e){}
    writeCookie(COOKIE_NAME, theme, 400);
    applyTheme(theme);
    updateToggleIcon(theme);
  }

  function toggle(){ setTheme(getStoredTheme() === 'dark' ? 'light' : 'dark'); }

  function ensureToggle(){
    try{
      var container = document.querySelector('#topbar-stats');
      var wrap = container ? container.parentElement : null; // .ml-auto
      if(!wrap) return;
      if(document.getElementById('theme-toggle-btn')) return;
      var btn = document.createElement('button');
      btn.id = 'theme-toggle-btn';
      btn.className = 'btn btn-sm btn-outline-secondary';
      btn.style.display = 'inline-flex';
      btn.style.alignItems = 'center';
      btn.style.justifyContent = 'center';
      btn.style.width = '36px';
      btn.style.height = '36px';
      btn.style.borderRadius = '6px';
      btn.style.padding = '0';
      btn.style.lineHeight = '1';
      btn.title = 'Toggle dark theme';
      btn.innerHTML = '<i class="mdi mdi-weather-night"></i>';
      btn.addEventListener('click', function(e){ e.preventDefault(); toggle(); });
      wrap.appendChild(btn);
      updateToggleIcon(getStoredTheme());
    }catch(e){}
  }

  function updateToggleIcon(theme){
    try{
      var btn = document.getElementById('theme-toggle-btn');
      if(!btn) return;
      var isDark = theme === 'dark';
      btn.innerHTML = isDark ? '<i class="mdi mdi-white-balance-sunny"></i>' : '<i class="mdi mdi-weather-night"></i>';
      btn.title = isDark ? 'Switch to light theme' : 'Switch to dark theme';
    }catch(e){}
  }

  function init(){
    var t = getStoredTheme();
    applyTheme(t);
    if(document.readyState === 'loading'){
      document.addEventListener('DOMContentLoaded', ensureToggle);
    }else{ ensureToggle(); }
  }

  return { init: init, set: setTheme, toggle: toggle, get: getStoredTheme };
})();


