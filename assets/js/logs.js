Distribyted.logs = {
    // Keep streaming via fetch; add jQuery-based polling fallback for older browsers if needed
    loadView: function () {
        if (window.ReadableStream && window.fetch) {
            fetch("/api/log")
                .then(function(response){
                    if (response.ok && response.body) { return response.body.getReader(); }
                    throw new Error('HTTP '+response.status);
                })
                .then(function(reader){
                    var decoder = new TextDecoder();
                    var lastString = '';
                    function processText(res){
                        var done = res.done, value = res.value;
                        if (done) { return; }
                        var string = '' + lastString + decoder.decode(value);
                        var lines = string.split(/\r\n|[\r\n]/g);
                        lastString = lines.pop() || '';
                        lines.forEach(function(element){
                            try {
                                var json = JSON.parse(element);
                                var properties = "";
                                Object.keys(json).forEach(function(key){
                                    if (key === 'level' || key === 'component' || key === 'message' || key === 'time') return;
                                    properties += '<b>' + key + '</b>=' + json[key] + ' ';
                                });
                                var tableClass = '';
                                switch (json.level) {
                                    case 'error': tableClass = 'table-danger'; break;
                                    case 'warn': tableClass = 'table-warning'; break;
                                    case 'debug': tableClass = 'table-info'; break;
                                    default: tableClass = ''; break;
                                }
                                var tr = '<tr class="' + tableClass + '"><td>' + new Date(json.time*1000).toLocaleString() + '</td><td>' + json.level + '</td><td>' + json.component + '</td><td>' + json.message + '</td><td>' + properties + '</td></tr>';
                                $('#log_table').append(tr);
                            } catch (err) { /* ignore */ }
                        });
                        return reader.read().then(processText);
                    }
                    return reader.read().then(processText);
                })
                .catch(function(){ /* ignore */ });
            return;
        }
        // Polling fallback (rarely used)
        var lastSize = 0;
        function poll(){
            $.ajax({ url: '/api/log', method: 'GET' })
              .then(function(text){
                  if(typeof text !== 'string') return;
                  if(text.length <= lastSize){ lastSize = text.length; return; }
                  var chunk = text.substring(lastSize);
                  lastSize = text.length;
                  chunk.split(/\r\n|[\r\n]/g).forEach(function(line){
                      try{
                          var json = JSON.parse(line);
                          var properties = '';
                          Object.keys(json).forEach(function(k){ if(k==='level'||k==='component'||k==='message'||k==='time') return; properties += '<b>'+k+'</b>='+json[k]+' '; });
                          var cls = json.level==='error'?'table-danger': json.level==='warn'?'table-warning': json.level==='debug'?'table-info':'';
                          $('#log_table').append('<tr class="'+cls+'"><td>'+new Date(json.time*1000).toLocaleString()+'</td><td>'+json.level+'</td><td>'+json.component+'</td><td>'+json.message+'</td><td>'+properties+'</td></tr>');
                      }catch(e){}
                  });
              })
              .always(function(){ setTimeout(poll, 2000); });
        }
        poll();
    }
}
