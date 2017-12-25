TimeSync = (function() {
  var client;
  var samples = [];
  var computedOffset = 0;
  var lastLoggedAt = null;

  function addSample(sample) {
    if (samples.length >= 10) {
      samples.shift();
    }

    samples.push(sample);
    updateOffset();
  }

  function average(items) {
    return items.reduce((a, b) => a+b, 0) / items.length;
  }

  function updateOffset(sample) {
    var sortedSamples = samples.slice(0).sort((a, b) => {
      if (a.rtt < b.rtt) {
        return -1;
      } else if (a.rtt > b.rtt) {
        return 1;
      } else {
        return 0;
      }
    });

    var samplesToAverage = samples.slice(0, 3);
    computedOffset = average(samplesToAverage.map(s => s.offset));

    if (lastLoggedAt == null || (Math.abs(lastLoggedAt - computedOffset) > 100)) {
      lastLoggedAt = computedOffset;
      console.log('New timesync offset:', computedOffset)
    }
  }

  function takeSample() {
    var startTime = Date.now();

    client.get('/currentTime')
      .then((timeStr) => {
        var serverTime = Number(timeStr);
        if (Number.isNaN(serverTime)) {
          console.error('/currentTime returned an invalid number:', timeStr);
          return;
        }

        var endTime = Date.now();
        var rtt = endTime - startTime;
        var offset = (startTime + rtt/2) - serverTime;

        addSample({ rtt, offset });

        if (samples.length < 10) {
          setTimeout(takeSample, 200);
        }
      })
  }

  return {
    start(huntClient) {
      client = huntClient;
      takeSample();
      setInterval(takeSample, 2000);
    },

    getTime() {
      return Date.now() - computedOffset;
    }
  }
})();
