// jQuery-based settings page logic
// Loads initial state and binds form submissions

(function(){
  var gCfg = null;

  function loadConfig(){
    return Distribyted.http.getJSON('/api/settings/config')
      .then(function(j){ gCfg = j || {}; return gCfg; })
      .catch(function(){ gCfg = {}; return gCfg; });
  }

  function initValues(){
    // Watch interval
    Distribyted.http.getJSON('/api/watch_interval').then(function(j){ if(j && j.interval){ $('#watch-interval-settings').val(j.interval); } });
    // Limits
    Distribyted.http.getJSON('/api/settings/limits').then(function(j){ if(j){ $('#dl-mbit').val(j.downloadMbit||0); $('#ul-mbit').val(j.uploadMbit||0); } });
    // qBt toggle
    Distribyted.http.getJSON('/api/settings/qbt').then(function(j){ if(j){ $('#qbt-enabled').prop('checked', !!j.enabled); } });
    // Health
    Distribyted.http.getJSON('/api/settings/health').then(function(j){
      if(!j) return;
      $('#health-enabled').prop('checked', !!j.enabled);
      $('#health-interval').val(j.intervalMinutes || 60);
      $('#health-grace').val(j.graceMinutes || 30);
      $('#health-min-seeders').val(j.minSeeders || 2);
      $('#health-good-seeders').val(j.goodSeeders || 5);
      $('#health-excellent-seeders').val(j.excellentSeeders || 10);
      (j.arr || []).forEach(addArrRow);
    });

    // General config snapshot
    loadConfig().then(function(c){
      try{
        if(c && c.http){
          $('#http-ip').val(c.http.ip || '');
          $('#http-port').val(c.http.port || '');
          $('#http-httpfs').prop('checked', !!c.http.httpfs);
        }
        if(c && c.webdav){
          $('#wd-port').val(c.webdav.port || '');
          $('#wd-user').val(c.webdav.user || '');
          $('#wd-pass').val(c.webdav.pass || '');
        }
        if(c && c.torrent){
          $('#tor-addto').val(c.torrent.add_timeout || c.torrent.addTimeout || '');
          $('#tor-readto').val(c.torrent.read_timeout || c.torrent.readTimeout || '');
          $('#tor-continue').prop('checked', !!(c.torrent.continue_when_add_timeout || c.torrent.continueWhenAddTimeout));
          $('#tor-cache').val(c.torrent.global_cache_size || c.torrent.globalCacheSize || '');
          $('#tor-meta').val(c.torrent.metadata_folder || c.torrent.metadataFolder || '');
          $('#tor-noipv6').prop('checked', !!c.torrent.disable_ipv6 || !!c.torrent.disableIPv6);
          $('#tor-notcp').prop('checked', !!c.torrent.disable_tcp || !!c.torrent.disableTCP);
          $('#tor-noutp').prop('checked', !!c.torrent.disable_utp || !!c.torrent.disableUTP);
          $('#tor-ip').val(c.torrent.ip || '');
          $('#tor-pool').val(c.torrent.reader_pool_size || c.torrent.readerPoolSize || 4);
          $('#tor-port').val(c.torrent.listen_port || c.torrent.listenPort || 0);
          $('#tor-readahead').val(c.torrent.readahead_mb || c.torrent.readaheadMB || 2);
          try{
            var et = (c.torrent.extra_trackers || c.torrent.extraTrackers || []).join('\n');
            $('#tor-extra-trackers').val(et);
            $('#tor-extra-trackers-url').val(c.torrent.extra_trackers_url || c.torrent.extraTrackersURL || '');
          }catch(e){}
        }
        if(c && c.fuse){
          $('#fuse-allow').prop('checked', !!c.fuse.allow_other || !!c.fuse.allowOther);
          $('#fuse-path').val(c.fuse.path || '');
        }
        if(c && c.log){
          $('#log-debug').prop('checked', !!c.log.debug);
          $('#log-path').val(c.log.path || '');
          $('#log-backups').val(c.log.max_backups || c.log.maxBackups || '');
          $('#log-size').val(c.log.max_size || c.log.maxSize || '');
          $('#log-age').val(c.log.max_age || c.log.maxAge || '');
        }
      }catch(e){}
    });
  }

  function setActiveSettingsSection(section){
    try{
      var sec = (section||'').trim() || 'general';
      var activeId = 'tab-' + sec;
      var panes = document.querySelectorAll('.tab-content .tab-pane');
      panes.forEach(function(p){
        p.classList.remove('show');
        p.classList.remove('active');
      });
      var el = document.getElementById(activeId);
      if(!el){ el = document.getElementById('tab-general'); }
      if(el){ el.classList.add('show'); el.classList.add('active'); }
    }catch(e){}
  }

  function postConfig(body){
    return Distribyted.http.postJSON('/api/settings/config', body);
  }

  // Arr management
  function addArrRow(item){
    var $row = $('<div class="row g-2 align-items-center mb-2">\
      <div class="col-auto">\
      <select class="form-select arr-type">\
        <option value="radarr">Radarr</option>\
        <option value="sonarr">Sonarr</option>\
        <option value="lidarr">Lidarr</option></select></div>\
      <div class="col-auto"><input type="text" class="form-control arr-name" placeholder="Name" style="min-width:140px"></div>\
      <div class="col-auto"><input type="text" class="form-control arr-url" placeholder="Base URL (e.g. http://127.0.0.1:7878)" style="min-width:320px"></div>\
      <div class="col-auto"><input type="password" class="form-control arr-key" placeholder="API Key" style="min-width:280px"></div>\
      <div class="col-auto form-check"><input type="checkbox" class="form-check-input arr-insecure" id="arr-insec"> <label class="form-check-label" for="arr-insec">Insecure</label></div>\
      <div class="col-auto"><button type="button" class="btn btn-info me-2 arr-test">Test</button></div>\
      <div class="col-auto"><button type="button" class="btn btn-danger arr-del">Delete</button></div></div>');
    if(item){
      $row.find('.arr-type').val(item.type || 'radarr');
      $row.find('.arr-name').val(item.name || '');
      $row.find('.arr-url').val(item.base_url || item.baseUrl || '');
      $row.find('.arr-key').val(item.api_key || item.apiKey || '');
      $row.find('.arr-insecure').prop('checked', !!item.insecure);
    }
    $row.on('click', '.arr-del', function(){ $row.remove(); });
    $row.on('click', '.arr-test', function(){
      var type = $row.find('.arr-type').val();
      var name = ($row.find('.arr-name').val() || '').trim();
      var baseUrl = ($row.find('.arr-url').val() || '').trim();
      var apiKey = ($row.find('.arr-key').val() || '').trim();
      var insecure = !!$row.find('.arr-insecure').prop('checked');
      if(!baseUrl || !apiKey){ Distribyted.message.error('Base URL and API Key required'); return; }
      Distribyted.http.postJSON('/api/settings/health/arr/test', { type: type, name: name, base_url: baseUrl, api_key: apiKey, insecure: insecure })
        .then(function(){
          Distribyted.message.info('Connection OK');
          if(confirm('Save this Arr instance now?')){
            try{
              var arr = collectArrRows();
              var found = arr.find(function(it){ return it.base_url===baseUrl && it.type===type; });
              var inst = { type: type, name: name, base_url: baseUrl, api_key: apiKey, insecure: insecure };
              if(!found){ arr.push(inst); }
              var body = {
                enabled: !!$('#health-enabled').prop('checked'),
                intervalMinutes: parseInt($('#health-interval').val(), 10) || 60,
                graceMinutes: parseInt($('#health-grace').val(), 10) || 30,
                minSeeders: parseInt($('#health-min-seeders').val(), 10) || 0,
                goodSeeders: parseInt($('#health-good-seeders').val(), 10) || 0,
                excellentSeeders: parseInt($('#health-excellent-seeders').val(), 10) || 0,
                arr: arr
              };
              postConfig({}); // no-op to ensure handler exists
              $.ajax({ url: '/api/settings/health', method: 'POST', contentType: 'application/json', data: JSON.stringify(body) })
                .then(function(){ Distribyted.message.info('Arr saved.'); })
                .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'Save failed'; Distribyted.message.error(msg); });
            }catch(e){ Distribyted.message.error(e.message||'Save failed'); }
          }
        })
        .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'Connection failed'; Distribyted.message.error(msg); });
    });
    $('#arr-list').append($row);
  }

  function collectArrRows(){
    var out = [];
    $('#arr-list > .row').each(function(){
      var $r = $(this);
      var type = $r.find('.arr-type').val();
      var name = ($r.find('.arr-name').val()||'').trim();
      var baseUrl = ($r.find('.arr-url').val()||'').trim();
      var apiKey = ($r.find('.arr-key').val()||'').trim();
      var insecure = !!$r.find('.arr-insecure').prop('checked');
      if(baseUrl && apiKey){ out.push({ type: type, name: name, base_url: baseUrl, api_key: apiKey, insecure: insecure }); }
    });
    return out;
  }

  // Bindings
  $(document).on('submit', '#watch-interval-form-settings', function(e){
    e.preventDefault();
    var v = parseInt($('#watch-interval-settings').val(), 10);
    if (isNaN(v) || v <= 0) { Distribyted.message.error('Invalid interval'); return; }
    Distribyted.http.postJSON('/api/watch_interval', { interval: v })
      .then(function(){ Distribyted.message.info('Watcher interval updated.'); })
      .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'update failed'; Distribyted.message.error(msg); });
  });

  $(document).on('submit', '#limits-form', function(e){
    e.preventDefault();
    var dl = parseFloat($('#dl-mbit').val()) || 0;
    var ul = parseFloat($('#ul-mbit').val()) || 0;
    Distribyted.http.postJSON('/api/settings/limits', { downloadMbit: dl, uploadMbit: ul })
      .then(function(){ Distribyted.message.info('Limits updated.'); })
      .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'save failed'; Distribyted.message.error(msg); });
  });

  $(document).on('submit', '#qbt-form', function(e){
    e.preventDefault();
    var enabled = !!$('#qbt-enabled').prop('checked');
    Distribyted.http.postJSON('/api/settings/qbt', { enabled: enabled })
      .then(function(){ Distribyted.message.info('qBittorrent API setting saved.'); })
      .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'save failed'; Distribyted.message.error(msg); });
  });

  $(document).on('click', '#arr-add', function(){ addArrRow(); });

  $(document).on('submit', '#health-form', function(e){
    e.preventDefault();
    var arr = collectArrRows();
    var body = {
      enabled: !!$('#health-enabled').prop('checked'),
      intervalMinutes: parseInt($('#health-interval').val(), 10) || 60,
      graceMinutes: parseInt($('#health-grace').val(), 10) || 30,
      minSeeders: parseInt($('#health-min-seeders').val(), 10) || 0,
      goodSeeders: parseInt($('#health-good-seeders').val(), 10) || 0,
      excellentSeeders: parseInt($('#health-excellent-seeders').val(), 10) || 0,
      arr: arr
    };
    if (body.intervalMinutes < 60) { Distribyted.message.error('Minimum interval is 60 minutes'); return; }
    $.ajax({ url: '/api/settings/health', method: 'POST', contentType: 'application/json', data: JSON.stringify(body) })
      .then(function(){ Distribyted.message.info('Health settings saved.'); })
      .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'save failed'; Distribyted.message.error(msg); });
  });

  $(document).on('submit', '#http-form', function(e){
    e.preventDefault(); if(!gCfg) return;
    var http = $.extend({}, gCfg.http || {});
    http.ip = ($('#http-ip').val()||'').trim();
    http.port = parseInt($('#http-port').val(), 10) || 0;
    http.httpfs = !!$('#http-httpfs').prop('checked');
    postConfig({ http: http }).then(function(){ gCfg.http = http; Distribyted.message.info('HTTP settings saved.'); })
      .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'save failed'; Distribyted.message.error(msg); });
  });

  $(document).on('submit', '#webdav-form', function(e){
    e.preventDefault(); if(!gCfg) return;
    var wd = $.extend({}, gCfg.webdav || {});
    wd.port = parseInt($('#wd-port').val(), 10) || 0;
    wd.user = ($('#wd-user').val()||'').trim();
    wd.pass = ($('#wd-pass').val()||'').trim();
    postConfig({ webdav: wd }).then(function(){ gCfg.webdav = wd; Distribyted.message.info('WebDAV settings saved.'); })
      .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'save failed'; Distribyted.message.error(msg); });
  });

  $(document).on('submit', '#torrent-form', function(e){
    e.preventDefault(); if(!gCfg) return;
    var tor = $.extend({}, gCfg.torrent || {});
    tor.addTimeout = parseInt($('#tor-addto').val(), 10) || 0;
    tor.readTimeout = parseInt($('#tor-readto').val(), 10) || 0;
    tor.continueWhenAddTimeout = !!$('#tor-continue').prop('checked');
    tor.globalCacheSize = parseInt($('#tor-cache').val(), 10) || 0;
    tor.metadataFolder = ($('#tor-meta').val()||'').trim();
    tor.disableIPv6 = !!$('#tor-noipv6').prop('checked');
    tor.disableTCP = !!$('#tor-notcp').prop('checked');
    tor.disableUTP = !!$('#tor-noutp').prop('checked');
    tor.ip = ($('#tor-ip').val()||'').trim();
    tor.readerPoolSize = parseInt($('#tor-pool').val(), 10) || 1;
    tor.listenPort = parseInt($('#tor-port').val(), 10) || 0;
    tor.readaheadMB = parseInt($('#tor-readahead').val(), 10) || 0;
    var etxt = ($('#tor-extra-trackers').val() || '').split(/\r?\n/).map(function(s){return s.trim();}).filter(Boolean);
    tor.extraTrackers = etxt;
    tor.extraTrackersURL = ($('#tor-extra-trackers-url').val()||'').trim();
    postConfig({ torrent: tor }).then(function(){ gCfg.torrent = tor; Distribyted.message.info('Torrent settings saved.'); })
      .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'save failed'; Distribyted.message.error(msg); });
  });

  $(document).on('submit', '#fuse-form', function(e){
    e.preventDefault(); if(!gCfg) return;
    var fu = $.extend({}, gCfg.fuse || {});
    fu.allowOther = !!$('#fuse-allow').prop('checked');
    fu.path = ($('#fuse-path').val()||'').trim();
    postConfig({ fuse: fu }).then(function(){ gCfg.fuse = fu; Distribyted.message.info('FUSE settings saved.'); })
      .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'save failed'; Distribyted.message.error(msg); });
  });

  $(document).on('submit', '#log-form', function(e){
    e.preventDefault(); if(!gCfg) return;
    var lg = $.extend({}, gCfg.log || {});
    lg.debug = !!$('#log-debug').prop('checked');
    lg.path = ($('#log-path').val()||'').trim();
    lg.maxBackups = parseInt($('#log-backups').val(), 10) || 0;
    lg.maxSize = parseInt($('#log-size').val(), 10) || 0;
    lg.maxAge = parseInt($('#log-age').val(), 10) || 0;
    postConfig({ log: lg }).then(function(){ gCfg.log = lg; Distribyted.message.info('Log settings saved.'); })
      .catch(function(xhr){ var msg=(xhr&&xhr.responseJSON&&xhr.responseJSON.error)||'save failed'; Distribyted.message.error(msg); });
  });

  // Initialize on ready
  $(function(){
    initValues();
    // Activate section from hash on initial load and when hash changes
    setActiveSettingsSection((location.hash||'').replace('#',''));
    window.addEventListener('hashchange', function(){ setActiveSettingsSection((location.hash||'').replace('#','')); });
  });
})();


