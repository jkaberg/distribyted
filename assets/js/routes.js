Handlebars.registerHelper("torrent_status", function (chunks, totalPieces) {
    const pieceStatus = {
        "H": { class: "bg-warning", tooltip: "checking pieces" },
        "P": { class: "bg-info", tooltip: "" },
        "C": { class: "bg-success", tooltip: "downloaded pieces" },
        "W": { class: "bg-transparent" },
        "?": { class: "bg-danger", tooltip: "erroed pieces" },
    };
    const list = Array.isArray(chunks) ? chunks : [];
    const tp = typeof totalPieces === 'number' ? totalPieces : 0;
    const chunksAsHTML = list.map(chunk => {
        const n = (chunk && typeof chunk.numPieces === 'number') ? chunk.numPieces : 0;
        const percentage = tp * n / 100;
        const pcMeta = pieceStatus[chunk.status]
        const pieceStatusClass = pcMeta.class;
        const pieceStatusTip = pcMeta.tooltip;

        const div = document.createElement("div");
        div.className = "progress-bar " + pieceStatusClass;
        div.setAttribute("role", "progressbar");

        if (pieceStatusTip) {
            div.setAttribute("data-toggle", "tooltip");
            div.setAttribute("data-placement", "top");
            div.setAttribute("title", pieceStatusTip);
        }

        div.style.cssText = "width: " + percentage + "%";

        return div.outerHTML;
    });

    return '<div class="progress">' + chunksAsHTML.join("\n") + '</div>'
});

Handlebars.registerHelper("torrent_info", function (peers, seeders, pieceSize) {
    const MB = 1048576;

    var messages = [];

    var errorLevels = [];
    const seedersMsg = "- Number of seeders is too low (" + seeders + ")."
    if (seeders < 2) {
        errorLevels[0] = 2;
        messages.push(seedersMsg);
    } else if (seeders >= 2 && seeders < 4) {
        errorLevels[0] = 1;
        messages.push(seedersMsg);
    } else {
        errorLevels[0] = 0;
    }

    const pieceSizeMsg = "- Piece size is too big (" + Humanize.bytes(pieceSize, 1024) + "). Recommended size is 1MB or less."
    if (pieceSize <= MB) {
        errorLevels[1] = 0;
    } else if (pieceSize > MB && pieceSize < (MB * 4)) {
        errorLevels[1] = 1;
        messages.push(pieceSizeMsg);
    } else {
        errorLevels[2] = 2;
        messages.push(pieceSizeMsg);
    }

    const level = ["text-success", "text-warning", "text-danger"];
    const icon = ["mdi-check", "mdi-alert", "mdi-alert-octagram"];
    const div = document.createElement("div");
    const i = document.createElement("i");

    const errIndex = Math.max(...errorLevels);

    i.className = "mdi " + icon[errIndex];
    i.title = messages.join("\n");

    var ps = (typeof pieceSize === 'number') ? pieceSize : 0;
    var pr = (typeof peers === 'number') ? peers : 0;
    var sr = (typeof seeders === 'number') ? seeders : 0;
    const text = document.createTextNode(pr + "/" + sr + " (" + Humanize.bytes(ps, 1024) + " chunks) ");

    div.className = level[errIndex];
    div.appendChild(text);
    div.appendChild(i);

    return div.outerHTML;
});

// Generic helpers used by templates
Handlebars.registerHelper("lt", function (a, b) {
    var av = Number(a) || 0;
    var bv = Number(b) || 0;
    return av < bv;
});

Handlebars.registerHelper("or", function () {
    var args = Array.prototype.slice.call(arguments, 0, arguments.length - 1);
    for (var i = 0; i < args.length; i++) {
        if (!!args[i]) return true;
    }
    return false;
});

// Health cell helper (peers/seeders, seeders drive health state)
Handlebars.registerHelper("health_cell", function (peers, seeders) {
    var pr = (typeof peers === 'number') ? peers : Number(peers) || 0;
    var sr = (typeof seeders === 'number') ? seeders : Number(seeders) || 0;
    var display = sr + '/' + pr; // seeders/peers
    if (sr <= 0) {
        return '<span class="text-danger" title="Unhealthy: ' + display + '"><i class="mdi mdi-close-circle"></i> ' + display + '</span>';
    } else if (sr < 2) {
        return '<span class="text-warning" title="Weak: ' + display + '"><i class="mdi mdi-alert"></i> ' + display + '</span>';
    }
    return '<span class="text-success" title="Healthy: ' + display + '"><i class="mdi mdi-check-circle"></i> ' + display + '</span>';
});

Distribyted.routes = {
    _template: null,
    _interval: 5,
    _pauseUntil: 0,
    _pages: {},
    _mounted: false,
    _timer: null,
    _lastRouteKey: "",

    _isBusy: function () {
        // Avoid re-render during file selection/upload
        var inputs = document.querySelectorAll('.upload-form input[type="file"]');
        for (var i = 0; i < inputs.length; i++) {
            var f = inputs[i];
            if (f.files && f.files.length > 0) {
                return true;
            }
        }
        if (this._pauseUntil && Date.now() < this._pauseUntil) return true;
        return false;
    },

    _pause: function(ms){
        var now = Date.now();
        this._pauseUntil = Math.max(this._pauseUntil, now + (ms || 0));
    },

    _getTemplate: function () {
        if (this._template != null) {
            return this._template;
        }
        // Prefer inline template if available
        var el = document.getElementById('routes-template');
        if (el && el.innerHTML) {
            try{
                var compiled = Handlebars.compile(el.innerHTML);
                this._template = Promise.resolve(compiled);
                return this._template;
            }catch(e){ /* fallback to fetch */ }
        }
        // Fallback: fetch legacy external template if inline not found
        const tTemplate = fetch('/assets/templates/routes.html')
            .then(function(response){ if(response.ok) return response.text(); throw new Error('HTTP '+response.status); })
            .then(function(t){ return Handlebars.compile(t); })
            .catch(function(error){ Distribyted.message.error('Error getting routes template: ' + error.message); });
        this._template = tTemplate;
        return tTemplate;
    },

    _getRoutesJson: function () {
        return fetch('/api/routes')
            .then(function (response) {
                if (response.ok) {
                    return response.json();
                } else {
                    Distribyted.message.error('Error getting data from server. Response: ' + response.status)
                }
            }).then(function (routes) {
                routes = routes || [];
                // Expect lightweight objects: { name, folder, total }
                return routes.filter(function(r){ return r && r.name; });
            })
            .catch(function (error) {
                Distribyted.message.error('Error getting status info: ' + error.message)
            });
    },

    _routeKey: function(routes){
        try{
            var names = (routes||[]).map(function(r){ return r && r.name ? r.name : ''; }).filter(Boolean).sort();
            return names.join('|');
        }catch(e){ return ''; }
    },

    _updateTotals: function(routes){
        (routes||[]).forEach(function(r){
            if(!(r && r.name)) return;
            var el = document.getElementById('total-' + r.name);
            if(el && String(el.textContent) !== String(r.total||0)){
                el.textContent = String(r.total||0);
            }
        });
    },

    _getWatchInterval: function () {
        return fetch('/api/watch_interval')
            .then(function (response) {
                if (response.ok) {
                    return response.json();
                } else {
                    Distribyted.message.error('Error getting watch interval. Response: ' + response.status)
                }
            }).then(function (json) {
                if (json && json.interval)
                    Distribyted.routes._interval = json.interval
                return json;
            })
            .catch(function (error) {
                Distribyted.message.error('Error getting watch interval: ' + error.message)
            });
    },

	// Pagination helpers
	_gotoPage: function(route, page, totalPages){
		if(typeof page !== 'number'){ page = parseInt(page, 10) || 1; }
		if(totalPages && totalPages > 0){
			if(page < 1) page = 1;
			if(page > totalPages) page = totalPages;
		}
		this._pages[route] = page;
		this._renderRoutePage(route);
	},
	_renderPager: function(route, page, size, total){
		var ul = document.getElementById('pagination-' + route);
		var pager = document.getElementById('pager-' + route);
		var totalPages = Math.max(1, Math.ceil((Number(total)||0) / (Number(size)||1)));
		if(!pager){ return; }
		if(totalPages <= 1){ pager.style.display = 'none'; return; } else { pager.style.display = ''; }
		if(!ul){
			// nothing to render into
			return;
		}
		function li(label, targetPage, opts){
			opts = opts || {};
			var disabled = opts.disabled ? ' disabled' : '';
			var active = opts.active ? ' active' : '';
			var data = (typeof targetPage === 'number') ? (' data-page="' + String(targetPage) + '"') : '';
			var href = (typeof targetPage === 'number') ? '#' : 'javascript:void(0)';
			var aria = opts.aria ? (' aria-label="' + opts.aria + '"') : '';
			return '<li class="page-item' + disabled + active + '"><a class="page-link" href="' + href + '"' + data + aria + '>' + label + '</a></li>';
		}
		var html = '';
		// First / Prev (chevrons for consistent sizing)
		html += li('«', 1, { disabled: page <= 1, aria: 'First' });
		html += li('‹', page - 1, { disabled: page <= 1, aria: 'Previous' });
		// Page window with ellipses
		var windowSize = 5;
		var start = Math.max(1, page - Math.floor(windowSize/2));
		var end = Math.min(totalPages, start + windowSize - 1);
		start = Math.max(1, Math.min(start, end - windowSize + 1));
		if(start > 1){
			html += li('1', 1, { active: page === 1 });
			if(start > 2){ html += '<li class="page-item disabled"><span class="page-link">…</span></li>'; }
		}
		for(var p = start; p <= end; p++){
			html += li(String(p), p, { active: p === page });
		}
		if(end < totalPages){
			if(end < totalPages - 1){ html += '<li class="page-item disabled"><span class="page-link">…</span></li>'; }
			html += li(String(totalPages), totalPages, { active: page === totalPages });
		}
		// Next / Last (chevrons)
		html += li('›', page + 1, { disabled: page >= totalPages, aria: 'Next' });
		html += li('»', totalPages, { disabled: page >= totalPages, aria: 'Last' });
		ul.innerHTML = html;
		// Bind click handlers
		var links = ul.querySelectorAll('a.page-link[data-page]');
		var self = this;
		Array.prototype.forEach.call(links, function(a){
			a.addEventListener('click', function(e){
				e.preventDefault();
				var tp = parseInt(a.getAttribute('data-page'), 10);
				if(!isNaN(tp)){
					self._gotoPage(route, tp, totalPages);
				}
			});
		});
	},

    deleteTorrent: function (route, torrentHash) {
        if(!confirm('Delete this torrent?')) { return Promise.resolve(); }
        var url = '/api/routes/' + encodeURIComponent(route) + '/torrent/' + torrentHash

        return fetch(url, {
            method: 'DELETE'
        })
            .then(function (response) {
                if (response.ok) {
                    Distribyted.message.info('Torrent deleted.')
                    Distribyted.routes.loadView();
                } else {
                    response.json().then(json => {
                        Distribyted.message.error('Error deletting torrent. Response: ' + json.error)
                    })
                }
            })
            .catch(function (error) {
                Distribyted.message.error('Error deletting torrent: ' + error.message)
            });
    },

    blacklistTorrent: function(route, torrentHash){
        if(!confirm('Blacklist and re-request this item from Arr?')) { return Promise.resolve(); }
        var url = '/api/routes/' + encodeURIComponent(route) + '/torrent/' + torrentHash + '/blacklist'
        return fetch(url, { method: 'POST' })
            .then(function(response){
                if(response.ok){
                    Distribyted.message.info('Blacklisted and removed.');
                    Distribyted.routes.loadView();
                }else{
                    response.json().then(json => {
                        Distribyted.message.error('Error blacklisting: ' + (json.error || 'unknown'))
                    })
                }
            })
            .catch(function (error) {
                Distribyted.message.error('Error blacklisting: ' + error.message)
            });
    },

    showDetails: function(route, hash, name){
        var url = '/api/routes/' + encodeURIComponent(route) + '/torrent/' + encodeURIComponent(hash);
        fetch(url)
            .then(function(response){ return response.json(); })
            .then(function(data){
                if(!data || !data.stats){
                    Distribyted.message.error('No details available');
                    return;
                }
                var ts = data.stats || {};
                var fuse = (data.paths && data.paths.fuse) ? (data.paths.fuse + '/' + (ts.name || '')) : '';
                var httpfs = (data.paths && data.paths.httpfs) ? (data.paths.httpfs + '/' + (ts.name || '')) : '';
                var html = '' +
                    '<div class="modal fade" id="torrentModal" tabindex="-1" role="dialog" aria-hidden="true">' +
                    '  <div class="modal-dialog modal-lg" role="document">' +
                    '    <div class="modal-content">' +
                    '      <div class="modal-header">' +
                    '        <h5 class="modal-title">' + (name || ts.name || '') + '</h5>' +
                    '        <button type="button" class="close" data-dismiss="modal" aria-label="Close"><span aria-hidden="true">&times;</span></button>' +
                    '      </div>' +
                    '      <div class="modal-body">' +
                    '        <ul class="nav nav-tabs" role="tablist">' +
                    '          <li class="nav-item"><a class="nav-link active" id="info-tab" data-toggle="tab" href="#tab-info" role="tab">Info</a></li>' +
                    '          <li class="nav-item"><a class="nav-link" id="files-tab" data-toggle="tab" href="#tab-files" role="tab">Files</a></li>' +
                    '        </ul>' +
                    '        <div class="tab-content" style="padding-top:12px;">' +
                    '          <div class="tab-pane fade show active" id="tab-info" role="tabpanel" aria-labelledby="info-tab">' +
                    '            <div class="container-fluid">' +
                    '              <div class="row"><div class="col-3 font-weight-bold">Name</div><div class="col-9">' + (ts.name || '') + '</div></div>' +
                    '              <div class="row"><div class="col-3 font-weight-bold">Hash</div><div class="col-9"><code>' + (ts.hash || '') + '</code></div></div>' +
                    '              <div class="row"><div class="col-3 font-weight-bold">Size</div><div class="col-9">' + (ts.sizeBytes?Humanize.bytes(ts.sizeBytes,1024):'') + '</div></div>' +
                    '              <div class="row"><div class="col-3 font-weight-bold">Added at</div><div class="col-9">' + (ts.addedAt?new Date(ts.addedAt*1000).toLocaleString():'') + '</div></div>' +
                    '              <div class="row"><div class="col-3 font-weight-bold">Seeders/Peers</div><div class="col-9">' + (ts.seeders||0) + '/' + (ts.peers||0) + '</div></div>' +
                    '              <div class="row"><div class="col-3 font-weight-bold">Downloaded/Uploaded</div><div class="col-9">' + Humanize.bytes(ts.downloadedBytes||0,1024) + ' / ' + Humanize.bytes(ts.uploadedBytes||0,1024) + '</div></div>' +
                    '              <div class="row"><div class="col-3 font-weight-bold">Piece size</div><div class="col-9">' + Humanize.bytes(ts.pieceSize||0, 1024) + '</div></div>' +
                    '              <div class="row"><div class="col-3 font-weight-bold">Route folder</div><div class="col-9"><code>' + (data.folder || '') + '</code></div></div>' +
                    '              <div class="row"><div class="col-3 font-weight-bold">FUSE path</div><div class="col-9"><code>' + fuse + '</code></div></div>' +
                    '              <div class="row"><div class="col-3 font-weight-bold">HTTPFS path</div><div class="col-9"><a href="' + httpfs + '" target="_blank">' + httpfs + '</a></div></div>' +
                    '            </div>' +
                    '          </div>' +
                    '          <div class="tab-pane fade" id="tab-files" role="tabpanel" aria-labelledby="files-tab">' +
                    '            <div id="files-tree" class="mb-2"></div>' +
                    '          </div>' +
                    '        </div>' +
                    '      </div>' +
                    '      <div class="modal-footer">' +
                    '        <button type="button" class="btn btn-secondary" data-dismiss="modal">Close</button>' +
                    '      </div>' +
                    '    </div>' +
                    '  </div>' +
                    '</div>';
                // Remove any previous modal
                var old = document.getElementById('torrentModal');
                if(old && old.parentNode){ old.parentNode.removeChild(old); }
                var div = document.createElement('div');
                div.innerHTML = html;
                document.body.appendChild(div.firstChild);
                $('#torrentModal').modal('show');
                // Load files when Files tab is shown (lazy)
                $('a#files-tab').on('shown.bs.tab', function(){
                    var tree = document.getElementById('files-tree');
                    if(tree && !tree.getAttribute('data-loaded')){
                        fetch('/api/routes/' + encodeURIComponent(route) + '/torrent/' + encodeURIComponent(hash) + '/files')
                          .then(function(r){ return r.json(); })
                          .then(function(payload){
                              var files = (payload && Array.isArray(payload.files)) ? payload.files : [];
                              tree.innerHTML = Distribyted.routes.renderTree(files);
                              tree.setAttribute('data-loaded','1');
                          }).catch(function(err){
                              tree.innerHTML = '<div class="text-danger">Error loading files: ' + err.message + '</div>';
                          });
                    }
                });
            })
            .catch(function(err){
                Distribyted.message.error('Error loading details: ' + err.message)
            })
    },
    // Build a fully-expanded tree HTML from flat file paths
    renderTree: function(files){
        // Build nested structure using only children + isFile flags
        var root = { children: {} };
        files.forEach(function(f){
            var p = (f && f.path) ? f.path : '';
            var size = (f && typeof f.length === 'number') ? f.length : 0;
            var segs = (p || '').split(/[\\\/]+/).filter(Boolean);
            var cur = root;
            for(var i=0;i<segs.length;i++){
                var s = segs[i];
                if(!cur.children[s]){ cur.children[s] = { children: {}, isFile: false, size: 0 }; }
                // mark leaf as file
                if(i === segs.length-1){ cur.children[s].isFile = true; cur.children[s].size = size; }
                cur = cur.children[s];
            }
        });
        function renderNode(node){
            var html = '<ul class="list-unstyled ml-2">';
            Object.keys(node.children).sort().forEach(function(name){
                var n = node.children[name];
                if(n.isFile && Object.keys(n.children).length === 0){
                    html += '<li><span class="mdi mdi-file-outline mr-1"></span>' + name + ' <span class="text-muted">(' + Humanize.bytes(n.size||0, 1024) + ')</span></li>';
                } else {
                    html += '<li><span class="mdi mdi-folder-outline mr-1"></span>' + name;
                    html += renderNode(n);
                    html += '</li>';
                }
            });
            html += '</ul>';
            return html;
        }
        return renderNode(root);
    },

    // UI routes APIs
    triggerFileDialog: function(route){
        var input = document.getElementById('file-' + route);
        if(input){ input.click(); }
    },
    onFileSelected: function(event, route){
        var input = event.target;
        if(!input || !input.files || input.files.length === 0){
            return;
        }
        var fd = new FormData();
        fd.append('file', input.files[0]);
        fetch('/api/routes/' + encodeURIComponent(route) + '/files', {
            method: 'POST',
            body: fd
        }).then(function(response){
            if(response.ok){
                Distribyted.message.info('File uploaded');
                input.value = '';
                Distribyted.routes.loadView();
            } else {
                response.json().then(json => {
                    Distribyted.message.error('Error uploading: ' + json.error)
                })
            }
        }).catch(function(error){
            Distribyted.message.error('Error uploading: ' + error.message)
        })
    },
    addMagnet: function(route){
        var magnet = window.prompt('Paste magnet URL');
        if(!magnet){ return; }
        var url = '/api/routes/' + encodeURIComponent(route) + '/torrent';
        var body = JSON.stringify({ magnet: magnet });
        fetch(url, { method: 'POST', body: body })
            .then(function(response){
                if(response.ok){
                    Distribyted.message.info('New magnet added.');
                    Distribyted.routes.loadView();
                } else {
                    response.json().then(json => {
                        Distribyted.message.error('Error adding new magnet: ' + json.error)
                    }).catch(function(){
                        Distribyted.message.error('Error adding new magnet: ' + response.status)
                    })
                }
            }).catch(function(error){
                Distribyted.message.error('Error adding new magnet: ' + error.message)
            })
    },
    createRoute: function(name){
        return fetch('/api/routes', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: name })
        }).then(function(response){
            if(response.ok){
                Distribyted.message.info('Route created');
                Distribyted.routes.loadView();
            } else {
                response.json().then(json => {
                    Distribyted.message.error('Error creating route: ' + json.error)
                })
            }
        }).catch(function(error){
            Distribyted.message.error('Error creating route: ' + error.message)
        });
    },
    deleteRoute: function(name){
        return fetch('/api/routes/' + encodeURIComponent(name), { method: 'DELETE' })
            .then(function(response){
                if(response.ok){
                    Distribyted.message.info('Route deleted');
                    // Optimistically remove from DOM to avoid stale view
                    var cards = document.querySelectorAll('.card .card-header h2');
                    for (var i = 0; i < cards.length; i++) {
                        var h2 = cards[i];
                        if (h2 && h2.textContent && h2.textContent.indexOf(name) !== -1) {
                            var cc = h2.closest('.card');
                            if (cc && cc.parentNode) {
                                cc.parentNode.remove();
                            }
                        }
                    }
                    Distribyted.routes.loadView();
                } else {
                    response.json().then(json => {
                        Distribyted.message.error('Error deleting route: ' + json.error)
                    })
                }
            }).catch(function(error){
                Distribyted.message.error('Error deleting route: ' + error.message)
            });
    },
    // Pagination controls
    pagePrev: function(route){
        var p = this._pages[route] || 1;
        if (p > 1) {
            this._pages[route] = p - 1;
            this._renderRoutePage(route);
        }
    },
    pageNext: function(route){
        var p = this._pages[route] || 1;
        this._pages[route] = p + 1;
        this._renderRoutePage(route);
    },
	_renderRoutePage: function(route){
		var p = this._pages[route] || 1;
		var size = 25;
		var self = this;
		fetch('/api/routes/' + encodeURIComponent(route) + '/torrents?page=' + p + '&size=' + size)
			.then(function(response){ return response.json(); })
			.then(function(data){
				var tbody = document.getElementById('tbody-' + route);
				var pager = document.getElementById('pager-' + route);
				if(!tbody){ return; }
				var items = (data && Array.isArray(data.items)) ? data.items : [];
				// Render/Update pagination
				if (pager && data) {
					var currentPage = Number(data.page) || p;
					self._pages[route] = currentPage;
					self._renderPager(route, currentPage, Number(data.size)||size, Number(data.total)||0);
				}
                // Diff and update rows in-place to avoid flashing
                var desiredIds = new Set();
                var toAppend = [];
                function ensureRow(hash){
                    var id = 'tr-' + hash;
                    var tr = document.getElementById(id);
                    if(!tr){
                        tr = document.createElement('tr');
                        tr.id = id;
                        // 6 cells: name, dl/up, size, health, status, actions
                        // Build stable DOM nodes to minimize reflows
                        var tdName = document.createElement('td');
                        var a = document.createElement('a');
                        a.href = '#';
                        a.onclick = function(e){ e.preventDefault(); };
                        tdName.appendChild(a);
                        tr.appendChild(tdName);

                        tr.appendChild(document.createElement('td')); // dl/up
                        tr.appendChild(document.createElement('td')); // size
                        tr.appendChild(document.createElement('td')); // health (html)
                        tr.appendChild(document.createElement('td')); // status (html)

                        var tdActions = document.createElement('td');
                        var iBlk = document.createElement('i');
                        iBlk.className = 'mdi mdi-24px mdi-block-helper mr-2';
                        iBlk.title = 'blacklist and re-request';
                        tdActions.appendChild(iBlk);
                        var iDel = document.createElement('i');
                        iDel.className = 'mdi mdi-24px mdi-delete-forever';
                        iDel.title = 'delete torrent';
                        tdActions.appendChild(iDel);
                        tr.appendChild(tdActions);

                        toAppend.push(tr);
                    }
                    desiredIds.add(id);
                    return tr;
                }
                function setText(el, text){ if(el && el.textContent !== text){ el.textContent = text; } }
                // Apply all DOM writes in a single frame
                window.requestAnimationFrame(function(){
                items.forEach(function(ts){
                    var tr = ensureRow(ts.hash);
                    var fullName = ts.name || '';
                    var shortName = fullName;
                    if(shortName.length > 70){ shortName = shortName.substring(0,67) + '...'; }
                    var safeName = String(fullName).replace(/'/g, "\\'");
                    var nameCell = tr.children[0];
                    var a = nameCell.firstChild;
                    if(a){
                        var titleText = fullName.replace(/&/g,'&amp;').replace(/</g,'&lt;');
                        if(a.textContent !== shortName){ a.textContent = shortName; }
                        if(a.getAttribute('title') !== titleText){ a.setAttribute('title', titleText); }
                        // Set click handler once with current route/hash/name
                        a.onclick = function(e){ e.preventDefault(); Distribyted.routes.showDetails(route, ts.hash, safeName); };
                    }

                    var dlupCell = tr.children[1];
                    var dlupText = Humanize.bytes(ts.downloadedBytes || 0, 1024) + ' / ' + Humanize.bytes(ts.uploadedBytes || 0, 1024);
                    setText(dlupCell, dlupText);

                    var sizeCell = tr.children[2];
                    var sizeText = (ts.sizeBytes && ts.sizeBytes > 0) ? Humanize.bytes(ts.sizeBytes, 1024) : '';
                    setText(sizeCell, sizeText);

                    var healthCell = tr.children[3];
                    var wantHealth = Handlebars.helpers.health_cell(ts.peers || 0, ts.seeders || 0);
                    if(healthCell.innerHTML !== wantHealth){ healthCell.innerHTML = wantHealth; }

                    var statusCell = tr.children[4];
                    var wantStatus = Handlebars.helpers.torrent_status(ts.pieceChunks || [], ts.totalPieces || 0);
                    if(statusCell.innerHTML !== wantStatus){ statusCell.innerHTML = wantStatus; }

                    var actionsCell = tr.children[5];
                    var blk = actionsCell.children[0];
                    var del = actionsCell.children[1];
                    if(blk){ blk.onclick = function(){ Distribyted.routes.blacklistTorrent(route, ts.hash); }; }
                    if(del){ del.onclick = function(){ Distribyted.routes.deleteTorrent(route, ts.hash); }; }
                });
                if(toAppend.length){
                    var frag = document.createDocumentFragment();
                    toAppend.forEach(function(tr){ frag.appendChild(tr); });
                    tbody.appendChild(frag);
                }
                // Remove rows that are no longer present
                Array.from(tbody.children).forEach(function(tr){ if(tr.id && !desiredIds.has(tr.id)){ tbody.removeChild(tr); }});
                });
            })
    },
    uploadTorrent: function(event, route, form){
        event.preventDefault();
        // Prefer explicit form parameter if provided
        if(!form) form = event.currentTarget || event.target;
        var input = form && form.querySelector ? form.querySelector('input[type="file"]') : null;
        if(!input || !input.files || input.files.length === 0){
            Distribyted.message.error('Select a .torrent file');
            return false;
        }
        var fd = new FormData();
        fd.append('file', input.files[0]);
        fetch('/api/routes/' + encodeURIComponent(route) + '/files', {
            method: 'POST',
            body: fd
        }).then(function(response){
            if(response.ok){
                Distribyted.message.info('File uploaded');
                input.value = '';
                Distribyted.routes.loadView();
            } else {
                response.json().then(json => {
                    Distribyted.message.error('Error uploading: ' + json.error)
                })
            }
        }).catch(function(error){
            Distribyted.message.error('Error uploading: ' + error.message)
        }).finally(function(){
            // Resume refresh shortly after upload attempt
            setTimeout(function(){ Distribyted.routes._pauseUntil = 0; }, 500);
        });
        return false;
    },
    // files list removed; torrents appear in the main table

    loadView: function () {
        var self = this;
        if (self._timer) { clearTimeout(self._timer); self._timer = null; }
        // Skip refresh if user is selecting/uploading files
        if (self._isBusy()) {
            self._timer = setTimeout(function(){ self.loadView(); }, Math.max(1000, self._interval * 1000));
            return;
        }
        self._getTemplate()
            .then(function(t){
                if (!t) { return; }
                return Promise.all([self._getRoutesJson(), self._getWatchInterval()])
                    .then(function(values){
                        var routes = values[0] || [];
                        if (self._isBusy()) {
                            return;
                        }
                        var target = document.getElementById('template_target');
                        if (!target) { return; }
                        var key = self._routeKey(routes);
                        if (!self._mounted) {
                            target.innerHTML = t({ routes: routes, interval: self._interval });
                            self._mounted = true;
                            self._lastRouteKey = key;
                            routes.forEach(function(r){
                                if(!(r && r.name)) return;
                                if (self._pages[r.name] == null) { self._pages[r.name] = 1; }
                                self._renderRoutePage(r.name);
                            });
                        } else if (self._lastRouteKey !== key) {
                            // Route set changed: re-render template once
                            target.innerHTML = t({ routes: routes, interval: self._interval });
                            self._lastRouteKey = key;
                            routes.forEach(function(r){
                                if(!(r && r.name)) return;
                                if (self._pages[r.name] == null) { self._pages[r.name] = 1; }
                                self._renderRoutePage(r.name);
                            });
                        } else {
                            // Same routes: just update totals and refresh pages in place
                            self._updateTotals(routes);
                            routes.forEach(function(r){ if(r && r.name){ self._renderRoutePage(r.name); } });
                        }
                    })
            })
            .catch(function(err){
                Distribyted.message.error('Error rendering routes: ' + (err && err.message ? err.message : err));
            })
            .finally(function(){
                self._timer = setTimeout(function(){ self.loadView(); }, Math.max(1000, self._interval * 1000));
            });
    }
}

// Watch interval form handling
$(document).on('submit', '#watch-interval-form', function (event) {
    event.preventDefault();
    let v = parseInt($("#watch-interval").val());
    if (isNaN(v) || v <= 0) {
        Distribyted.message.error('Invalid interval');
        return;
    }
    fetch('/api/watch_interval', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ interval: v })
    }).then(function (response) {
        if (response.ok) {
            Distribyted.routes._interval = v;
            Distribyted.message.info('Watcher interval updated.');
            Distribyted.routes.loadView();
        } else {
            response.json().then(json => {
                Distribyted.message.error('Error updating interval: ' + json.error)
            })
        }
    }).catch(function (error) {
        Distribyted.message.error('Error updating interval: ' + error.message)
    });
});

// Create route form
$(document).on('submit', '#create-route-form', function (event) {
    event.preventDefault();
    var name = document.getElementById('new-route-name').value.trim();
    if(!name){
        Distribyted.message.error('Route name required');
        return;
    }
    Distribyted.routes.createRoute(name);
});