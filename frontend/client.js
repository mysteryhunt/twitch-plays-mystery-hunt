var client;

var rateLimitEnabled = true;
var delayEnabled = true;
var status = 'ready'; // ready, submitting, or wait

function changeStatus(newStatus) {
    status = newStatus;
    document.getElementById('status-text').innerText = status;
}

function wait(time) {
    return new Promise((resolve, reject) => {
        setTimeout(resolve, time);
    });
}

function clientInit() {
    client = HuntJSClient.connect('tpmh', 'http://localhost:8000');

    function heartbeat() {
        client.post('/heartbeat')
            .then(r => console.log('/hearbeat succeeded'))
            .catch(e => console.error('/hearbeat failed', e));
    }

    heartbeat();
    setInterval(heartbeat, 2 * 60 * 1000);

    // Start up pubsub
    // client.post('/startFakeEmitters')
    //     .then(() => console.log('Fake emitters started'))
    //     .catch(e => console.log('/startFakeEmitters failed', e));

    client.subscribe('gameState', (newState) => {
        //console.log('New game state:', newState);
	handleState.apply(null, eval('['+newState+']'));
    });

    client.subscribe('notifications', msg => toastr.info(msg));
}

function getTime() {
    return Date.now();
}

function move(direction) {
    if (status === 'submitting') {
        toastr.error('Move already in progress');
        return;
    }

    if (status === 'wait') {
        toastr.error('You may only move once every 4 seconds');
        return;
    }

    var endpoint = rateLimitEnabled ? '/move' : '/moveUnlimited';
    var delay = delayEnabled ? 1000 : 0;
    var rateLimit = rateLimitEnabled ? (4000 - delay) : 0;

    changeStatus('submitting');

    wait(delay)
        .then(() => client.post(endpoint, { direction: direction }))
        .then(() => changeStatus('wait'))
        .then(() => wait(rateLimit))
        .then(() => changeStatus('ready'))
        .catch((e) => toastr.error(e.message))
}

function toggleRateLimiting() {
    rateLimitEnabled = !rateLimitEnabled;
    if (status === 'wait') {
        changeStatus('ready');
    }
    document.getElementById('rate-limit-indicator').innerText = String(rateLimitEnabled)
}

function toggleDelay() {
    delayEnabled = !delayEnabled;
    document.getElementById('delay-indicator').innerText = String(delayEnabled)
}

function delayEnabled() {
    delayEnabled = !delayEnabled;
    document.getElementById('delay-indicator').innerText = String(delayEnabled)
}
