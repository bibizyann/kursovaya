const $ = (id) => document.getElementById(id);
const stations = {
  'paste-printing': ['Нанесение пасты', '▤'], 'component-placement': ['SMD-монтаж', '▦'],
  'reflow-soldering': ['Оплавление', '♨'], 'optical-inspection': ['Оптический контроль', '◎'],
  'manual-assembly': ['Ручной монтаж', '⌁'], 'programming-test': ['Программирование и испытания', '⌘'],
};
const kinds = {'paste-printer':'Принтер паяльной пасты','component-placer':'Установщик компонентов','reflow-oven':'Печь оплавления','optical-inspector':'Оптическая инспекция','manual-soldering':'Паяльная станция',programmer:'Программатор','power-supply':'Источник питания',multimeter:'Мультиметр'};
const states = {Ready:['Готов','good'],Progressing:['Обновляется','warn'],Degraded:['Ошибка','bad'],Unknown:['Неизвестно','unknown'],NotConfigured:['Не в профиле','unknown'],Fresh:['Свежая','good'],Stale:['Устарела','warn'],Missing:['Нет данных','unknown']};
const readings = {temperature_celsius:['Температура печи','°C'],target_temperature_celsius:['Уставка температуры','°C'],temperature_ready:['Температура достигнута','boolean'],voltage_volts:['Напряжение','В'],target_voltage_volts:['Уставка напряжения','В'],current_amperes:['Ток','А'],current_limit_amperes:['Ограничение тока','А'],output_enabled:['Выход включён','boolean'],min_voltage_volts:['Нижний допуск','В'],max_voltage_volts:['Верхний допуск','В'],last_operation_duration_seconds:['Последняя операция','с']};
const recipeFields = {id:'Рецепт',product:'Изделие',operation:'Операция',program:'Программа',stencil:'Трафарет',fixture:'Оснастка',componentCount:'Количество компонентов',peakTemperatureC:'Пиковая температура, °C',solderTemperatureC:'Температура жала, °C',jointCount:'Количество соединений',firmware:'Прошивка',voltageV:'Напряжение, В',currentLimitA:'Ограничение тока, А',minVoltageV:'Нижний допуск, В',maxVoltageV:'Верхний допуск, В'};
const reasons = {KubernetesUnavailable:'Kubernetes недоступен',ProcessNotFound:'Техпроцесс ещё не создан',StatusNotCurrent:'Оператор ещё не подтвердил текущую конфигурацию',RevisionNotObserved:'Ожидаем целевую ревизию',ProcessDeleting:'Техпроцесс удаляется',WorkloadsNotReady:'Ожидаем готовность компонентов',RevisionReady:'Целевая ревизия готова',ApplyingRevision:'Применяется новая конфигурация',WorkloadsProgressing:'Компоненты проходят проверки готовности',TemplateNotFound:'Шаблон техпроцесса не найден',RolloutTimedOut:'Превышено время обновления',InvalidDesiredState:'Ошибка конфигурации'};
let snapshot = null, selectedStation = '', selectedDevice = '', busy = false, timer;
const number = new Intl.NumberFormat('ru-RU', {maximumFractionDigits:2});
const element = (tag, className='', text='') => { const n=document.createElement(tag); n.className=className; n.textContent=text; return n; };
const badge = (state, label) => { const [text,kind]=states[state] || states.Unknown; return element('span',`badge ${kind}`,label || text); };
const yesNo = (value) => value == null ? 'Неизвестно' : value ? 'Да' : 'Нет';
const stamp = (value) => value ? new Date(value).toLocaleString('ru-RU') : '—';
const boolBadge = (value) => badge(value == null ? 'Unknown' : value ? 'Ready' : 'Degraded',yesNo(value));
const allDevices = () => snapshot?.workstations.flatMap(s=>s.devices) || [];

function row(label, value) { const n=element('div','state-row'); n.append(element('span','muted',label),value); return n; }
function link(url, text) { const a=element('a','button',text); a.href=url; a.target='_blank'; a.rel='noreferrer'; return a; }
function readingValue(reading) { const [label,unit]=readings[reading.name] || [reading.name,reading.unit]; return {label,unit:unit==='boolean'?'':unit,value:unit==='boolean'?yesNo(reading.value===1):number.format(reading.value)}; }
function valuesList(entries) { const dl=element('dl'); for(const [label,value] of entries) { dl.append(element('dt','',label),element('dd','',value ?? '—')); } return dl; }
function detailSection(title, content) { const section=element('section','detail-section'); section.append(element('h3','',title),content); return section; }

function render() {
  const data=snapshot, devices=allDevices(), process=data.process;
  const processBadge=badge(process.state); processBadge.id='process-state'; $('process-state').replaceWith(processBadge);
  $('process-reason').textContent=reasons[process.reason] || process.message || process.reason || 'Состояние ещё не определено';
  $('profile').textContent=process.template || '—';
  $('revision').textContent=process.revision ? `Ревизия ${process.revision}` : 'Ревизия неизвестна';
  $('ready-count').replaceChildren(document.createTextNode(`${process.ready ?? '—'} `),element('small','',`/ ${process.desired ?? '—'}`));
  $('fresh-count').replaceChildren(document.createTextNode(`${devices.filter(d=>d.telemetry.state==='Fresh').length} `),element('small','','/ 8'));
  $('grafana').hidden=!data.grafanaUrl; if(data.grafanaUrl) $('grafana').href=data.grafanaUrl;
  const sources=[['Kubernetes',data.kubernetes],['Prometheus',data.prometheus],['Receiver',data.receiver]];
  $('sources').replaceChildren(...sources.map(([name,source])=>element('span',`source ${source.available?'available':''}`,`${name}: ${source.available?'доступен':'недоступен'}`)));
  $('updated').textContent=`Обновлено ${new Date(data.generatedAt).toLocaleTimeString('ru-RU')} · каждые 3 с`;
  const missing=sources.filter(([,s])=>!s.available).map(([name])=>name);
  $('connection').className=`notice ${missing.length?'':'connected'}`;
  $('connection').textContent=missing.length ? `Нет текущих данных: ${missing.join(', ')}. Доступные источники продолжают отображаться.` : 'Источники доступны. Готовность процесса и телеметрия показаны независимо.';
  renderRoute(); renderDevices();
  if($('detail').open) renderDetail();
}

function renderRoute() {
	const focused=document.activeElement?.dataset.station;
  $('route').replaceChildren(...snapshot.workstations.map((station,index)=>{
    const button=element('button',`route-step ${selectedStation===station.id?'active':''}`); button.type='button';
	button.dataset.station=station.id;
    button.setAttribute('aria-pressed',String(selectedStation===station.id));
    const count=station.devices.filter(d=>d.workload.state==='Ready').length;
    button.append(element('span','route-number',String(index+1).padStart(2,'0')),element('span','route-title',stations[station.id]?.[0] || station.id),element('span','route-status',`${count}/${station.devices.length} приложений готовы`));
    button.addEventListener('click',()=>{selectedStation=selectedStation===station.id?'':station.id; renderRoute(); renderDevices();});
    return button;
  }));
	if(focused) [...$('route').querySelectorAll('button')].find(b=>b.dataset.station===focused)?.focus({preventScroll:true});
}

function renderDevices() {
  const query=$('search').value.trim().toLocaleLowerCase('ru-RU'), filter=$('state-filter').value;
  const devices=allDevices().filter(d=>{
    const attention=d.workload.state!=='Ready' || d.telemetry.state!=='Fresh' || !d.telemetry.healthy || !d.telemetry.calibrated || !d.telemetry.setupComplete || d.revisionMatches===false;
    return (!selectedStation || d.workstation===selectedStation) && `${d.id} ${d.manufacturer} ${d.model} ${kinds[d.kind]}`.toLocaleLowerCase('ru-RU').includes(query) && (filter==='all' || (filter==='ready' && d.workload.state==='Ready') || (filter==='attention' && attention));
  });
  const focused=document.activeElement?.dataset.device;
  $('devices').replaceChildren(...devices.map(card));
  if(focused) [...$('devices').querySelectorAll('button')].find(b=>b.dataset.device===focused)?.focus({preventScroll:true});
  $('visible-count').textContent=devices.length; $('empty').hidden=devices.length!==0;
  $('reset').hidden=!selectedStation && !query && filter==='all';
}

function card(device) {
  const card=element('article','device-card'), top=element('div','card-top');
  top.append(element('span','device-icon',stations[device.workstation]?.[1] || '◈'),badge(device.workload.state));
  card.append(top,element('h3','',`${device.manufacturer} ${device.model}`),element('div','device-kind',kinds[device.kind] || device.kind));
  const status=element('div','states'); status.append(row('Телеметрия',badge(device.telemetry.state)),row('Исправность',boolBadge(device.telemetry.healthy)));
  card.append(status);
  const primary=device.telemetry.readings?.find(r=>r.name==='temperature_celsius' || r.name==='voltage_volts') || device.telemetry.readings?.find(r=>r.name==='last_operation_duration_seconds');
  const reading=element('div','reading');
  if(primary) { const value=readingValue(primary), line=element('div'); line.append(element('span','reading-value',value.value),element('span','reading-unit',value.unit)); reading.append(line,element('div','reading-label',value.label)); }
  else reading.append(element('span','reading-value','—'),element('div','reading-label','Нет текущего измерения'));
  card.append(reading);
  if(device.revisionMatches===false) card.append(element('p','mismatch','Наблюдается другая ревизия'));
  const bottom=element('div','card-bottom'), button=element('button','text-button','Подробнее ↗'); button.type='button'; button.dataset.device=device.id; button.setAttribute('aria-label',`Подробнее: ${device.manufacturer} ${device.model}`);
  button.addEventListener('click',()=>{selectedDevice=device.id; renderDetail(); $('detail').showModal();});
  bottom.append(element('span','mono',device.id),button); card.append(bottom); return card;
}

function renderDetail() {
  const d=allDevices().find(device=>device.id===selectedDevice); if(!d) return;
  $('detail-title').textContent=`${d.manufacturer} ${d.model}`;
  const body=$('detail-body'), scroll=$('detail').scrollTop;
	const focusedLink=body.contains(document.activeElement) ? document.activeElement.href : '';
  const identity=valuesList([['Устройство',d.id],['Рабочее место',stations[d.workstation]?.[0]],['Приложение',states[d.workload.state]?.[0]],['Образ ПО',d.workload.image]]);
  const telemetry=valuesList([['Телеметрия',states[d.telemetry.state]?.[0]],['Время наблюдения',stamp(d.telemetry.observedAt)],['Исправность',yesNo(d.telemetry.healthy)],['Подготовка',yesNo(d.telemetry.setupComplete)],['Калибровка',yesNo(d.telemetry.calibrated)],['Наблюдаемое изделие',d.telemetry.product],['Наблюдаемый рецепт',d.telemetry.recipeId],['Наблюдаемая ревизия',d.telemetry.revision],['Целевая ревизия',snapshot.process.revision],['Ревизии совпадают',yesNo(d.revisionMatches)]]);
  body.replaceChildren(detailSection('Устройство и размещение',identity),detailSection('Наблюдаемое состояние',telemetry));
  const pods=element('div'); for(const pod of d.workload.pods || []) pods.append(element('p','pod-line mono',`${pod.name}\n${pod.node || 'Узел ещё не назначен'} · ${pod.phase} · ${pod.terminating?'Завершается':pod.ready?'Ready':'Not ready'}${pod.reason?' · '+pod.reason:''}\nРевизия ${pod.revision || '—'}`));
  if(!pods.children.length) pods.append(element('p','muted','Нет подтверждённых данных о pod.'));
  body.append(detailSection('Экземпляры приложения',pods));
  if(d.workload.recipe) body.append(detailSection('Рецепт выбранного профиля',valuesList(Object.entries(d.workload.recipe).map(([key,value])=>[recipeFields[key] || key,String(value)]))));
  const measured=(d.telemetry.readings || []).map(r=>{const v=readingValue(r);return [v.label,`${v.value} ${v.unit}`];});
  for(const [key,label] of [['operations','Завершённые операции'],['errors','Ошибки операций'],['rejected','Отклонённые изделия']]) if(d.telemetry[key]!=null) measured.push([label,number.format(d.telemetry[key])]);
  body.append(detailSection('Текущие показания',measured.length?valuesList(measured):element('p','muted','Измерения отсутствуют. Пропуски не заменяются нулём.')));
  if(d.grafanaUrl) { const actions=element('div','detail-actions'); actions.append(link(d.grafanaUrl,'История в Grafana ↗')); body.append(actions); }
	if(focusedLink) [...body.querySelectorAll('a')].find(a=>a.href===focusedLink)?.focus({preventScroll:true});
  $('detail').scrollTop=scroll;
}

function invalidate() {
  if(snapshot) {
    snapshot.process={state:'Unknown',reason:'KubernetesUnavailable'};
    snapshot.kubernetes={available:false}; snapshot.prometheus={available:false}; snapshot.receiver={available:false};
    for(const d of allDevices()) { d.workload={state:'Unknown',pods:[]}; d.telemetry={state:'Unknown',readings:[]}; d.revisionMatches=null; }
    render();
  }
  $('connection').className='notice error'; $('connection').textContent='Не удалось обновить обзор. Текущее состояние неизвестно; подключение будет повторено автоматически.';
  $('updated').textContent='Нет актуального ответа API';
}

async function refresh() {
  if(busy) return; busy=true; clearTimeout(timer); $('refresh').disabled=true;
  const controller=new AbortController(), timeout=setTimeout(()=>controller.abort(),8000);
  try {
    const response=await fetch('/api/v1/overview',{cache:'no-store',signal:controller.signal});
    if(!response.ok) throw new Error('Overview unavailable');
    const data=await response.json();
    if(data.schemaVersion!==1 || !Array.isArray(data.workstations) || !data.process) throw new Error('Invalid overview');
    snapshot=data; render();
  } catch { invalidate(); }
  finally {clearTimeout(timeout);busy=false;$('refresh').disabled=false;timer=setTimeout(refresh,3000);}
}
$('refresh').addEventListener('click',refresh);
$('search').addEventListener('input',renderDevices);
$('state-filter').addEventListener('change',renderDevices);
$('reset').addEventListener('click',()=>{selectedStation='';$('search').value='';$('state-filter').value='all';renderRoute();renderDevices();});
$('close-detail').addEventListener('click',()=>$('detail').close());
document.addEventListener('visibilitychange',()=>{if(!document.hidden) refresh();});
refresh();
