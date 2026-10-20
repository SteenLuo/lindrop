(function(){'use strict';
var $=function(id){return document.getElementById(id)},state=null,target='',busy=false,paused=false,jobID='',selected=[],isFolder=false,lastDevices='',lastMessages='',toastTimer,joining=false;
var key=localStorage.getItem('linnode-key')||'',fragment=new URLSearchParams(location.hash.slice(1)),join=fragment.get('join');
var inlineTexts=Object.create(null),inlineRequests=Object.create(null),expanded=Object.create(null),drafts=Object.create(null),visibleLimit=40;
var webNoticeReady=false,webSeenTasks=Object.create(null),webUnread=Object.create(null);
var webPrefs={banner:true,vibrate:true,badges:true,title:true};try{var savedPrefs=JSON.parse(localStorage.getItem('linnode-notices')||'{}');Object.keys(webPrefs).forEach(function(k){if(typeof savedPrefs[k]==='boolean')webPrefs[k]=savedPrefs[k]})}catch(_){}
var prefInputs={banner:'noticeBanner',vibrate:'noticeVibrate',badges:'noticeBadges',title:'noticeTitle'};
Object.keys(prefInputs).forEach(function(k){var input=$(prefInputs[k]);input.checked=webPrefs[k];input.onchange=function(){webPrefs[k]=input.checked;localStorage.setItem('linnode-notices',JSON.stringify(webPrefs));if(!webPrefs.banner&&$('toast').classList.contains('actionable'))$('toast').hidden=true;clearWebUnread('');lastDevices='';if(state)renderDevices()}});
if(location.hash)history.replaceState(null,'',location.pathname);
function toast(s,action){$('toast').textContent=s;$('toast').hidden=false;$('toast').onclick=action||null;$('toast').classList.toggle('actionable',!!action);clearTimeout(toastTimer);toastTimer=setTimeout(function(){$('toast').hidden=true},action?6500:2400)}
function size(n){if(n<1024)return n+' B';var units=['KB','MB','GB','TB'],i=-1;do{n/=1024;i++}while(n>=1024&&i<3);return n.toFixed(1)+' '+units[i]}
function sleep(n){return new Promise(function(r){setTimeout(r,n)})}
async function api(path,body){var o={credentials:'same-origin',headers:{}};if(body!==undefined){o.method='POST';o.headers['Content-Type']='application/json';o.body=JSON.stringify(body)}var r=await fetch(path,o),v=await r.json();if(!r.ok){var e=new Error(v.error||'连接失败');e.status=r.status;throw e}return v}
function showPair(){$('pair').hidden=false;$('linkState').textContent='未连接'}
async function pair(data){joining=true;try{var c=await api('/api/pair',data);key=c.key;localStorage.setItem('linnode-key',key);localStorage.setItem('linnode-name',c.name);$('myName').value=c.name;$('profileToggle').title='手机名称和提醒设置：'+c.name;$('pair').hidden=true;if(data.join)target='';await refresh(true);if(data.join&&state){target=state.id;lastMessages='';renderDevices();renderMessages(true)}if(c.created){$('profilePanel').hidden=false;$('profileHint').hidden=false;$('myName').focus();$('myName').select();loadPhoneNames()}}catch(e){if(!data.rename){showPair();$('pairError').textContent=e.message}throw e}finally{joining=false}}
$('pairForm').onsubmit=async function(e){e.preventDefault();$('pairButton').disabled=true;try{await pair({code:$('pairCode').value,key:key,name:localStorage.getItem('linnode-name')||''})}catch(_){}finally{$('pairButton').disabled=false}};
function ownID(){return state?'web:'+state.id+':'+state.client.id:''}
function outgoing(t){return t.sender===ownID()||t.client===state.client.id}
function peerFor(t){return outgoing(t)?t.target:t.sender}
function peers(){
 var all=state.devices.filter(function(d){return d.id.indexOf('web:')!==0}).map(function(d){return Object.assign({},d)});
 var known=Object.create(null);all.forEach(function(d){known[d.id]=true});
 (state.tasks||[]).forEach(function(t){var id=peerFor(t);if(id&&!known[id]&&id.indexOf('web:')!==0){all.push({id:id,name:outgoing(t)?t.targetName:t.senderName,online:false});known[id]=true}});
 return all;
}
function canSend(){if(!state||!target)return false;return state.devices.some(function(d){return d.id===target&&d.online&&d.id.indexOf('web:')!==0})}
function webUnreadCount(peer){return Object.keys(webUnread).filter(function(id){return !peer||webUnread[id]===peer}).length}
function clearWebUnread(peer){Object.keys(webUnread).forEach(function(id){if(webUnread[id]===peer)delete webUnread[id]});var total=webUnreadCount('');document.title=total&&webPrefs.title?'邻传 · '+total+'条新消息':'邻传 · 对话'}
function updateWebNotices(first){
 var box=$('messages'),viewed=!document.hidden&&box.scrollHeight-box.scrollTop-box.clientHeight<65?target:'',arrived=[];
 (state.tasks||[]).forEach(function(t){if(outgoing(t))return;if(!webNoticeReady||first){webSeenTasks[t.id]=true;return}var ready=t.direction==='web'&&['browser','waiting_browser','done'].indexOf(t.status)!==-1;if(ready&&!webSeenTasks[t.id]){webSeenTasks[t.id]=true;webUnread[t.id]=peerFor(t);arrived.push(t)}});
 webNoticeReady=true;if(viewed)clearWebUnread(viewed);else clearWebUnread('');
 if(arrived.length){var t=arrived[0],peer=peerFor(t),device=peers().find(function(d){return d.id===peer}),message='收到 '+(device?device.name:'电脑')+' 的新'+(t.kind==='text'?'文字':'文件');if(webPrefs.banner)toast(message+' · 点击查看',function(){drafts[target]=$('textDraft').value;target=peer;localStorage.setItem('linnode-target-'+state.id,target);$('textDraft').value=drafts[target]||'';resizeDraft();clearWebUnread(peer);visibleLimit=40;lastMessages='';renderDevices();renderMessages(true);$('toast').hidden=true});try{if(webPrefs.vibrate&&navigator.vibrate)navigator.vibrate(100)}catch(_){}lastDevices=''}
}
function renderDevices(){
 var all=peers(),signature=JSON.stringify(all)+target+busy+JSON.stringify(webUnread)+webPrefs.badges;
 if(signature===lastDevices)return;lastDevices=signature;
 var select=$('peerSelect');select.textContent='';
 all.forEach(function(d){var option=document.createElement('option');option.value=d.id;var count=webPrefs.badges?webUnreadCount(d.id):0;option.textContent=(d.name||'离线设备').replace('（本机）','')+(count?' · '+count+'条新消息':'')+(d.online?'':' · 离线');select.appendChild(option)});
 select.value=target;select.disabled=busy||!all.length;
 var ready=canSend();$('sendText').disabled=busy||!ready;$('attachToggle').disabled=busy||!ready;
 $('linkState').textContent=ready?'在线':'离线';$('linkState').classList.toggle('offline',!ready);
 $('linkState').title='网页入口：'+state.name;
}
$('peerSelect').onchange=function(){drafts[target]=$('textDraft').value;target=this.value;localStorage.setItem('linnode-target-'+state.id,target);$('textDraft').value=drafts[target]||'';resizeDraft();visibleLimit=40;lastMessages='';renderDevices();renderMessages(true)};
async function refresh(force){
 if(joining&&!force)return;
 try{
  var s=await api('/api/state'),first=!state;state=s;$('pair').hidden=true;
  if(first){$('myName').value=s.client.name;var remembered=localStorage.getItem('linnode-target-'+s.id);target=peers().some(function(d){return d.id===remembered})?remembered:s.id}
  updateWebNotices(first);renderDevices();renderMessages(false);hydrateTexts();
 }catch(e){$('linkState').textContent='已断开';$('linkState').classList.add('offline');if(e.status===401)showPair()}
}
var statuses={preparing:'准备中',verifying:'校验中',waiting:'等待对方接收',waiting_remote:'等待对方接收',receiving:'接收中',sending:'发送中',paused:'已暂停',queued:'等待继续',done:'已送达',rejected:'已拒绝',browser:'待下载',waiting_browser:'待下载'};
function messageTime(t){var d=new Date(t.created*1000),today=new Date();return (d.toDateString()===today.toDateString()?'':(d.getMonth()+1)+'月'+d.getDate()+'日 ')+String(d.getHours()).padStart(2,'0')+':'+String(d.getMinutes()).padStart(2,'0')}
function conversation(){return (state.tasks||[]).filter(function(t){return peerFor(t)===target}).sort(function(a,b){var n=(a.createdOrder||a.created*1e9)-(b.createdOrder||b.created*1e9);return n||a.id.localeCompare(b.id)})}
function textFold(s){var chars=Array.from(s),lines=s.split(/\r?\n/);return {long:chars.length>240||lines.length>7,preview:chars.slice(0,240).join('')}}
function renderMessages(switching,anchor){
 if(!state)return;
 var all=conversation(),items=all.slice(-visibleLimit);
 var signature=target+':'+JSON.stringify(items)+':'+items.map(function(t){return (Object.prototype.hasOwnProperty.call(inlineTexts,t.id)?inlineTexts[t.id]:'')+':'+!!expanded[t.id]}).join('|')+':'+visibleLimit;
 if(signature===lastMessages)return;lastMessages=signature;
 var box=$('messages'),oldRows=Object.create(null),desired=[];box.querySelectorAll('[data-task-id]').forEach(function(el){oldRows[el.dataset.taskId]=el});var bottom=box.scrollHeight-box.scrollTop-box.clientHeight<65,oldTop=box.scrollTop;
 var firstVisible=Array.from(box.querySelectorAll('[data-task-id]')).find(function(el){return el.offsetTop+el.offsetHeight>box.scrollTop});
 var oldAnchor=firstVisible&&firstVisible.dataset.taskId,oldOffset=firstVisible?firstVisible.offsetTop-box.scrollTop:0;
 if(all.length>visibleLimit){var earlier=document.createElement('button');earlier.className='earlier';earlier.textContent='更早的消息';earlier.onclick=function(){visibleLimit+=40;lastMessages='';renderMessages(false)};desired.push(earlier)}
 if(!items.length){var empty=document.createElement('p');empty.className='empty';empty.textContent='发一段文字，或传一份文件';desired.push(empty)}
 var previous=0;
 items.forEach(function(t){
  if(t.created-previous>300){var stamp=document.createElement('div');stamp.className='time-separator';stamp.textContent=messageTime(t);desired.push(stamp)}previous=t.created;
  var renderKey=JSON.stringify(t)+':'+(inlineTexts[t.id]||'')+':'+!!expanded[t.id],old=oldRows[t.id];if(old&&(old.dataset.renderKey===renderKey||old.querySelector('video'))){desired.push(old);return}
  var sent=outgoing(t),row=document.createElement('article');row.dataset.renderKey=renderKey;row.className='message '+(sent?'outgoing':'incoming');row.dataset.taskId=t.id;row.dataset.direction=sent?'out':'in';
  var avatar=document.createElement('span');avatar.className='avatar';avatar.setAttribute('aria-hidden','true');avatar.textContent=sent?'我':'↔';row.appendChild(avatar);
  var bubble=document.createElement('div');bubble.className='bubble';
  if(t.kind==='text'){
   var body=document.createElement('pre');body.className='message-body';body.tabIndex=0;body.dataset.textTask=t.id;body.setAttribute('aria-label',sent?'发出的文字':'收到的文字');
   if(Object.prototype.hasOwnProperty.call(inlineTexts,t.id)){
    var full=inlineTexts[t.id],fold=textFold(full);body.textContent=expanded[t.id]?full:fold.preview;
    if(fold.long&&!expanded[t.id])body.classList.add('collapsed');bubble.appendChild(body);
    if(fold.long){var toggle=document.createElement('button');toggle.className='expand-text';toggle.textContent=expanded[t.id]?'收起':'展开全文';toggle.onclick=function(e){e.stopPropagation();expanded[t.id]=!expanded[t.id];lastMessages='';renderMessages(false,t.id)};bubble.appendChild(toggle)}
    var tools=document.createElement('div');tools.className='message-tools';var copy=document.createElement('button');copy.textContent='复制';copy.setAttribute('aria-label','复制这条文字');copy.onclick=function(e){e.stopPropagation();copyMessage(full)};tools.appendChild(copy);row.appendChild(tools);
   }else{body.textContent=t.status==='rejected'?'未接收的文字':'文字正在送达…';bubble.appendChild(body)}
   body.onclick=function(e){e.stopPropagation();var active=row.classList.contains('armed');document.querySelectorAll('.message.armed').forEach(function(el){el.classList.remove('armed')});row.classList.toggle('armed',!active)};
  }else{
   bubble.classList.add('file-bubble');
   var mediaURL='/api/media?id='+encodeURIComponent(t.id),hasPreview=(t.direction==='web'||t.direction==='in'&&t.status==='done');
   if(hasPreview&&(t.media==='image'||t.media==='video')){
    bubble.classList.add('media-bubble');var media=document.createElement(t.media==='image'?'img':'video');media.src=mediaURL;
    if(t.media==='image'){media.alt=t.title;media.loading='lazy';media.tabIndex=0;media.onclick=function(){openImage(mediaURL,t.title)};media.onkeydown=function(e){if(e.key==='Enter')openImage(mediaURL,t.title)}}else{media.controls=true;media.preload='metadata';media.playsInline=true}
    bubble.appendChild(media);
   }else{var glyph=document.createElement('span');glyph.className='file-glyph '+(t.folder?'folder':'document');glyph.setAttribute('aria-hidden','true');bubble.appendChild(glyph)}
   var title=document.createElement('strong');title.className='file-name';title.textContent=t.title;var info=document.createElement('small');info.textContent=size(t.total)+(t.status==='done'?'':' · '+(statuses[t.status]||t.status));bubble.appendChild(title);bubble.appendChild(info);
   if(!sent&&t.direction==='web'){var download=document.createElement('a');download.className='download';download.href='/api/download?inbox='+encodeURIComponent(t.id);download.textContent='下载';download.onclick=function(){toast('请在浏览器下载列表查看文件')};bubble.appendChild(download)}
  }
  row.appendChild(bubble);
  if(t.error||(sent&&t.status!=='done')){var status=document.createElement('small');status.className='message-status';status.textContent=t.error||(statuses[t.status]||t.status);row.appendChild(status)}
  desired.push(row);
 });
 var keep=new Set(desired);Array.from(box.children).forEach(function(el){if(!keep.has(el))el.remove()});desired.forEach(function(el,index){if(box.children[index]!==el)box.insertBefore(el,box.children[index]||null)});
 if(switching){box.scrollTop=box.scrollHeight;return}
 var node=Array.from(box.querySelectorAll('[data-task-id]')).find(function(el){return el.dataset.taskId===(anchor||oldAnchor)});
 if(anchor&&node){box.scrollTop=Math.max(0,node.offsetTop-16)}else if(bottom){box.scrollTop=box.scrollHeight}else if(node){box.scrollTop=node.offsetTop-oldOffset}else{box.scrollTop=oldTop}
}
function openImage(url,title){$('imageFull').src=url;$('imageFull').alt=title;$('imagePreview').hidden=false;$('closeImage').focus()}
$('closeImage').onclick=function(){$('imagePreview').hidden=true;$('imageFull').removeAttribute('src')};
$('imagePreview').onclick=function(e){if(e.target===$('imagePreview'))$('closeImage').click()};document.addEventListener('keydown',function(e){if(e.key==='Escape'&&!$('imagePreview').hidden)$('closeImage').click()});
function hydrateTexts(){
 $('messages').querySelectorAll('[data-text-task]').forEach(function(preview){
  var id=preview.dataset.textTask;if(Object.prototype.hasOwnProperty.call(inlineTexts,id)||inlineRequests[id])return;
  inlineRequests[id]=api('/api/text?id='+encodeURIComponent(id)).then(function(v){inlineTexts[id]=v.text;lastMessages='';renderMessages(false)}).catch(function(){if(preview.isConnected)preview.textContent='文字暂未到达，正在重试…'}).finally(function(){delete inlineRequests[id]});
 });
}
async function copyMessage(s){
 var copied=false;try{if(navigator.clipboard&&navigator.clipboard.writeText){await navigator.clipboard.writeText(s);copied=true}}catch(_){}
 if(!copied){var input=document.createElement('textarea');input.value=s;input.className='clipboard-helper';document.body.appendChild(input);input.focus();input.select();try{copied=document.execCommand('copy')}catch(_){}input.remove()}
 toast(copied?'已复制':'请长按文字选择并复制');
}
$('profileToggle').onclick=function(){$('profilePanel').hidden=!$('profilePanel').hidden;$('attachMenu').hidden=true};
$('saveName').onclick=async function(){try{await pair({key:key,name:$('myName').value,rename:true});$('profilePanel').hidden=true;$('profileHint').hidden=true;toast('手机名称已保存')}catch(e){toast(e.message)}};
async function loadPhoneNames(){var button=$('findPhoneNames');button.disabled=true;button.textContent='正在查找局域网名称…';try{var names=await api('/api/phone-names');var select=$('knownPhoneName'),labels=Object.create(null);select.textContent='';(names||[]).forEach(function(p){var label=p.name+' · '+p.entry;if(labels[label])return;labels[label]=true;var option=document.createElement('option');option.value=p.ref;option.textContent=label;select.appendChild(option)});$('phoneNameChoices').hidden=!select.options.length;button.textContent=select.options.length?'刷新局域网名称':'未找到已登记名称，点击重试'}catch(e){button.textContent='暂不可用，点击重试'}finally{button.disabled=false}}
$('findPhoneNames').onclick=loadPhoneNames;
$('usePhoneName').onclick=async function(){var ref=$('knownPhoneName').value;if(!ref)return;this.disabled=true;try{var result=await api('/api/phone-name',{ref:ref});localStorage.setItem('linnode-name',result.name);$('myName').value=result.name;$('profileToggle').title='手机名称和提醒设置：'+result.name;$('profileHint').hidden=true;toast('已使用这个手机名称');await refresh(true)}catch(e){toast(e.message)}finally{this.disabled=false}};
$('attachToggle').onclick=function(){$('attachMenu').hidden=!$('attachMenu').hidden};
document.addEventListener('click',function(e){if(!e.target.closest('.attach-wrap'))$('attachMenu').hidden=true;if(!e.target.closest('.message'))document.querySelectorAll('.message.armed').forEach(function(el){el.classList.remove('armed')})});
function resizeDraft(){var el=$('textDraft');el.style.height='auto';el.style.height=Math.min(126,Math.max(40,el.scrollHeight))+'px'}
$('textDraft').oninput=resizeDraft;
$('textDraft').onkeydown=function(e){if(e.key==='Enter'&&e.ctrlKey&&!e.isComposing){e.preventDefault();$('textSendForm').requestSubmit()}};
$('textSendForm').onsubmit=async function(e){e.preventDefault();if(busy){toast('请先完成或暂停当前任务');return}if(!canSend()){toast('请选择在线电脑');return}var value=$('textDraft').value,chosen=target;if(!value.trim())return;if(value.indexOf('\u0000')!==-1){toast('文字包含无效字符');return}var file=new File([value],'文字消息-'+Date.now()+'.txt',{type:'text/plain;charset=utf-8'});if(file.size>65536){toast('文字最多64 KB，请改为发送文件');return}if(await start([file],false,'text',value)){if(target===chosen&&$('textDraft').value===value){$('textDraft').value='';drafts[chosen]='';resizeDraft()}}};
function read(blob){return new Promise(function(resolve,reject){var r=new FileReader();r.onload=function(){resolve(r.result)};r.onerror=reject;r.readAsArrayBuffer(blob)})}async function hashFile(file){var h=sha256.create();for(var offset=0;offset<file.size;offset+=262144){if(paused)throw new Error('已暂停，请重新选择相同文件继续');h.update(await read(file.slice(offset,offset+262144)));$('progressText').textContent='正在校验源文件 · '+file.name+' · '+Math.min(100,Math.round((offset+262144)/Math.max(1,file.size)*100))+'%'}return h.hex()}
function choose(folder){if(busy){toast('请先完成或暂停当前任务');return}if(!state){showPair();return}if(!canSend()){toast('请选择在线电脑');return}$('attachMenu').hidden=true;(folder?$('folder'):$('files')).click()}
$('choose').onclick=function(){choose(false)};$('chooseFolder').onclick=function(){choose(true)};
$('files').onchange=function(){start(Array.from(this.files),false);this.value=''};
$('folder').onchange=function(){start(Array.from(this.files),true);this.value=''};
$('messages').ondragover=function(e){e.preventDefault();this.classList.add('dragging')};
$('messages').ondragleave=function(){this.classList.remove('dragging')};
$('messages').ondrop=function(e){e.preventDefault();this.classList.remove('dragging');if(!busy&&canSend())start(Array.from(e.dataTransfer.files),false)};
$('pause').onclick=function(){paused=true;$('progressText').textContent='正在暂停，已接收的进度会保留'};
async function start(files,folder,kind,textBody){if(!files.length||busy)return;if(files.length>5000){toast('一次最多 5000 个文件');return}busy=true;paused=false;var completed=false;$('sendText').disabled=true;renderDevices();selected=files;var chosen=target;$('progress').hidden=kind==='text';$('choose').disabled=true;$('chooseFolder').disabled=true;$('jobLabel').textContent=files.length===1?files[0].name:files.length+' 个文件';var total=files.reduce(function(sum,f){return sum+f.size},0),metas=[],title=files.length===1?files[0].name:'手机文件-'+Date.now();try{for(var i=0;i<files.length;i++){var f=files[i];if(f.size>107374182400)throw new Error('单文件不能超过 100 GiB');var rel=f.name;if(folder&&f.webkitRelativePath){var parts=f.webkitRelativePath.split('/');title=parts.shift();rel=parts.join('/')}metas.push({rel:rel,size:f.size,sha:await hashFile(f)})}var recordKey='linnode-job-'+sha256(key+chosen+(kind||'')+JSON.stringify(metas)),stored=localStorage.getItem(recordKey)||'',result;while(true){if(paused)throw new Error('已暂停，重新选择相同文件与目标可继续');result=await api('/api/task/offer',{target:chosen,offer:{id:stored,title:title,folder:folder,files:metas,kind:kind||''}});stored=result.id;jobID=stored;localStorage.setItem(recordKey,stored);if(kind==='text'&&textBody!==undefined)inlineTexts[stored]=textBody;if(result.reply.status==='waiting'){$('progressText').textContent='等待 '+(state.devices.find(function(d){return d.id===chosen})||{}).name+' 确认接收…';await sleep(1500);continue}if(result.reply.status==='rejected')throw new Error('对方已拒绝接收');if(result.reply.status==='done'){$('bar').style.width='100%';$('progressText').textContent='这个任务已完成';completed=true;return true}if(result.reply.status!=='receiving')throw new Error('目标暂时无法接收');break}var done=result.reply.offsets.reduce(function(s,n){return s+n},0),began=Date.now(),initial=done;for(var index=0;index<files.length;index++){for(var offset=result.reply.offsets[index];offset<files[index].size;){if(paused)throw new Error('已暂停，重新选择相同文件与目标可继续');var blob=files[index].slice(offset,offset+262144),response=await fetch('/api/task/chunk?id='+stored+'&index='+index+'&offset='+offset,{method:'POST',credentials:'same-origin',headers:{'X-LinNode-Upload':'1','Content-Type':'application/octet-stream'},body:blob});var ack=await response.json();if(!response.ok)throw new Error(ack.error||'传输中断');done+=ack.offset-offset;offset=ack.offset;var rate=(done-initial)/Math.max(.1,(Date.now()-began)/1000),percent=total?Math.round(done/total*100):100;$('bar').style.width=percent+'%';$('progressText').textContent=percent+'% · '+size(done)+' / '+size(total)+' · '+size(rate)+'/s · 约剩 '+Math.max(0,Math.ceil((total-done)/Math.max(1,rate)))+' 秒'}}$('progressText').textContent='文件已传完，正在校验保存…';await api('/api/task/finish',{id:stored});$('bar').style.width='100%';$('progressText').textContent='已完整保存到目标电脑 ✓';localStorage.removeItem(recordKey);completed=true;if(kind!=='text')toast('文件已送达')}catch(e){$('progressText').textContent=e.message+'。已接收进度保留。';toast(e.message)}finally{busy=false;$('sendText').disabled=false;$('choose').disabled=false;$('chooseFolder').disabled=false;lastDevices='';await refresh();if(completed)setTimeout(function(){if(!busy)$('progress').hidden=true},2200)}return completed}


window.addEventListener('beforeunload',function(e){if(busy){e.preventDefault();e.returnValue='传输尚未完成'}});
if(join){pair({join:join,key:key,name:localStorage.getItem('linnode-name')||''}).catch(function(){})}else if(key){pair({key:key}).catch(function(){})}else{refresh()}
setInterval(refresh,2000);
})();
