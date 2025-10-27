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
            return this._template
        }

        const tTemplate = fetch('/assets/templates/routes.html')
            .then((response) => {
                if (response.ok) {
                    return response.text();
                } else {
                    Distribyted.message.error('Error getting data from server. Response: ' + response.status);
                }
            })
            .then((t) => {
                return Handlebars.compile(t);
            })
            .catch(error => {
                Distribyted.message.error('Error getting routes template: ' + error.message);
            });

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
                    '        <div class="container-fluid">' +
                    '          <div class="row"><div class="col-3 font-weight-bold">Hash</div><div class="col-9"><code>' + (ts.hash || '') + '</code></div></div>' +
                    '          <div class="row"><div class="col-3 font-weight-bold">Seeders/Peers</div><div class="col-9">' + (ts.seeders||0) + '/' + (ts.peers||0) + '</div></div>' +
                    '          <div class="row"><div class="col-3 font-weight-bold">Piece size</div><div class="col-9">' + Humanize.bytes(ts.pieceSize||0, 1024) + '</div></div>' +
                    '          <div class="row"><div class="col-3 font-weight-bold">Route folder</div><div class="col-9"><code>' + (data.folder || '') + '</code></div></div>' +
                    '          <div class="row"><div class="col-3 font-weight-bold">FUSE path</div><div class="col-9"><code>' + fuse + '</code></div></div>' +
                    '          <div class="row"><div class="col-3 font-weight-bold">HTTPFS path</div><div class="col-9"><a href="' + httpfs + '" target="_blank">' + httpfs + '</a></div></div>' +
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
            })
            .catch(function(err){
                Distribyted.message.error('Error loading details: ' + err.message)
            })
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
        fetch('/api/routes/' + encodeURIComponent(route) + '/torrents?page=' + p + '&size=' + size)
            .then(function(response){ return response.json(); })
            .then(function(data){
                var tbody = document.getElementById('tbody-' + route);
                var pageEl = document.getElementById('page-' + route);
                var pager = document.getElementById('pager-' + route);
                if(pageEl){ pageEl.textContent = data.page; }
                if(!tbody){ return; }
                var items = (data && Array.isArray(data.items)) ? data.items : [];
                if (pager) {
                    if (data && data.total > data.size) {
                        pager.style.display = '';
                    } else {
                        pager.style.display = 'none';
                    }
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
        // Skip refresh if user is selecting/uploading files
        if (this._isBusy()) {
            return;
        }
        this._getTemplate()
            .then(function(t){
                if (!t) { return; }
                return Promise.all([Distribyted.routes._getRoutesJson(), Distribyted.routes._getWatchInterval()])
                    .then(function(values){
                        const routes = values[0] || [];
                        if (Distribyted.routes._isBusy()) {
                            return;
                        }
                        var target = document.getElementById('template_target');
                        if (target) {
                            target.innerHTML = t({ routes: routes, interval: Distribyted.routes._interval });
                            // Initialize first page render per route
                            routes.forEach(function(r){
                                if(!(r && r.name)) return;
                                if (Distribyted.routes._pages[r.name] == null) {
                                    Distribyted.routes._pages[r.name] = 1;
                                }
                                Distribyted.routes._renderRoutePage(r.name);
                            })
                        }
                    })
            })
            .catch(function(err){
                Distribyted.message.error('Error rendering routes: ' + (err && err.message ? err.message : err));
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