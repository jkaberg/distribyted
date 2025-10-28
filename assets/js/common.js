Handlebars.registerHelper('ibytes', function (bytesSec, timePassed) {
    return Humanize.ibytes(bytesSec / timePassed, 1024);
});
Handlebars.registerHelper('bytes', function (bytes) {
    return Humanize.bytes(bytes, 1024);
});


var Distribyted = Distribyted || {};

Distribyted.message = {

    _toast: function(type, message){
        try{
            var wrap = document.getElementById('toast-wrap');
            if(!wrap){
                wrap = document.createElement('div');
                wrap.id = 'toast-wrap';
                wrap.className = 'toast-container position-fixed top-0 end-0 p-3';
                document.body.appendChild(wrap);
            }
            var el = document.createElement('div');
            el.className = 'toast align-items-center text-bg-' + (type==='error'?'danger':'info') + ' border-0';
            el.setAttribute('role','alert');
            el.setAttribute('aria-live','assertive');
            el.setAttribute('aria-atomic','true');
            el.innerHTML = '<div class="d-flex"><div class="toast-body">'+ String(message||'') +'</div><button type="button" class="btn-close btn-close-white me-2 m-auto" data-bs-dismiss="toast" aria-label="Close"></button></div>';
            wrap.appendChild(el);
            var t = new bootstrap.Toast(el, { delay: 5000 });
            t.show();
        }catch(e){ alert(String(message||'')); }
    },

    error: function (message) { this._toast('error', message); },
    info: function (message) { this._toast('info', message); }
}

// Lightweight jQuery-based HTTP helpers for consistent AJAX usage
Distribyted.http = {
    getJSON: function (url) {
        return $.ajax({ url: url, method: 'GET', dataType: 'json' });
    },
    postJSON: function (url, body) {
        return $.ajax({ url: url, method: 'POST', contentType: 'application/json', data: JSON.stringify(body || {}), dataType: 'json' });
    },
    delete: function (url) {
        return $.ajax({ url: url, method: 'DELETE' });
    },
    upload: function (url, formData) {
        return $.ajax({ url: url, method: 'POST', data: formData, processData: false, contentType: false });
    }
}

$(document).ready(function () {
    "use strict";
    // Highlight active nav links (both fixed sidebar and offcanvas)
    try{
        function normalizePath(p){ if(!p) return '/'; if(p.length>1 && p.endsWith('/')) return p.slice(0,-1); return p; }
        var currentPath = normalizePath(location.pathname||'/');
        var menus = [ document.getElementById('sidebar-menu'), document.getElementById('sidebar-menu-offcanvas') ];
        menus.forEach(function(menu){
            if(!menu) return;
            // Clear current
            var links = menu.querySelectorAll('a.nav-link');
            Array.prototype.forEach.call(links, function(a){ a.classList.remove('active'); });
            // Find best match by pathname (ignore hash)
            Array.prototype.forEach.call(links, function(a){
                try{
                    var url = new URL(a.getAttribute('href'), location.origin);
                    var linkPath = normalizePath(url.pathname);
                    if(currentPath === linkPath || (currentPath.startsWith('/settings') && linkPath === '/settings')){
                        a.classList.add('active');
                    }
                }catch(e){}
            });
        });
        // Highlight settings subsection by hash (both in fixed and offcanvas)
        var hash = (location.hash||'').replace('#','') || 'general';
        var sublinks = document.querySelectorAll('a[data-settings-section]');
        Array.prototype.forEach.call(sublinks, function(a){ a.classList.remove('active'); });
        var activeSubs = document.querySelectorAll('a[data-settings-section="'+hash+'"]');
        Array.prototype.forEach.call(activeSubs, function(a){ a.classList.add('active'); });
    }catch(e){}
});