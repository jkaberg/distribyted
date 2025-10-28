Distribyted.topbar = (function(){
    var cacheChart = null;
    function ensureChart(){
        if(!cacheChart){ cacheChart = new CacheChart('tb-cache'); }
        return cacheChart;
    }
    function updateSpeeds(stats){
        try{
            var dl = stats.torrentStats.downloadedBytes / Math.max(1e-6, stats.torrentStats.timePassed);
            var ul = stats.torrentStats.uploadedBytes / Math.max(1e-6, stats.torrentStats.timePassed);
            document.getElementById('tb-dl').innerText = Humanize.ibytes(dl, 1024) + '/s';
            document.getElementById('tb-ul').innerText = Humanize.ibytes(ul, 1024) + '/s';
        }catch(e){}
    }
    function updateCache(stats){
        try{
            ensureChart().update(stats.cacheFilled, stats.cacheCapacity - stats.cacheFilled);
            var used = Humanize.bytes(stats.cacheFilled * 1024 * 1024, 1024);
            var total = Humanize.bytes(stats.cacheCapacity * 1024 * 1024, 1024);
            var el = document.getElementById('tb-cache-wrap');
            if(el){ el.title = 'Cache available: ' + used + ' / ' + total; }
        }catch(e){}
    }
    function updateNet(net){
        try{
            document.getElementById('tb-ip').innerText = 'IP: ' + (net.publicIp || '...');
            var el = document.getElementById('tb-conn');
            el.innerText = net.connectible ? 'Connectible' : 'Not connectible';
            el.className = net.connectible ? 'badge badge-success' : 'badge badge-warning';
        }catch(e){}
    }
    function tick(){
        Distribyted.http.getJSON('/api/status').then(function(j){ if(j){ updateSpeeds(j); updateCache(j);} });
        Distribyted.http.getJSON('/api/net').then(function(j){ if(j){ updateNet(j);} });
    }
    function start(){ tick(); setInterval(tick, 2000); }
    return { start: start };
})();


