/* eslint arrow-body-style: 0 */

const HuntJS = require('huntjs-backend');
const request = require('request');

const DB = require('./db');

DB.init();

const artifactNames = [
  'Great Gi',
  'Majestic Mask',
  'Terrific Tabi',
];

function simPostCommand(path) {
  return new Promise((resolve, reject) => {
    request.post(`${process.env.SIMULATION_SERVICE_ADDRESS}${path}`, (error, httpResponse, body) => {
      const code = httpResponse && httpResponse.statusCode;

      if (error) {
        reject(error);
      } else if (code < 200 || code >= 400) {
        reject(new Error(`Got status code ${code} from ${process.env.SIMULATION_SERVICE_ADDRESS}${path}. Body: ${body}`));
      } else {
        resolve();
      }
    });
  }).catch(e => console.log(`Error requesting ${path}`, e));
}

function simGetValue(path) {
  return new Promise((resolve, reject) => {
    request.get(`${process.env.SIMULATION_SERVICE_ADDRESS}${path}`, (error, httpResponse, body) => {
      const code = httpResponse && httpResponse.statusCode;

      if (error) {
        reject(error);
      } else if (code < 200 || code >= 400) {
        reject(new Error(`Got status code ${code} from ${process.env.SIMULATION_SERVICE_ADDRESS}${path}. Body: ${body}`));
      } else {
        resolve(body);
      }
    });
  });
}

HuntJS.post('/move', async ({ team, data }) => {
  if (!data || !data.direction) {
    throw HuntJS.Error(422, 'No direction given');
  }

  if (!['N', 'E', 'S', 'W', 'NE', 'SE', 'SW', 'NW'].includes(data.direction)) {
    throw HuntJS.Error(422, 'Invalid direction');
  }

  console.log(`Got direction command: ${data.direction}`);
  await simPostCommand(`/${team.id()}/move/${data.direction}`);

  return {};
}, {
  sessionRateLimit: {
    limit: 1,
    window: 4, // seconds
  },
});

HuntJS.post('/moveUnlimited', async ({ team, data }) => {
  if (!data || !data.direction) {
    throw HuntJS.Error(422, 'No direction given');
  }

  if (!['N', 'E', 'S', 'W', 'NE', 'SE', 'SW', 'NW'].includes(data.direction)) {
    throw HuntJS.Error(422, 'Invalid direction');
  }

  console.log(`Got direction command: ${data.direction}`);
  await simPostCommand(`/${team.id()}/move/${data.direction}`);

  return {};
});

HuntJS.post('/heartbeat', async ({ team }) => {
  simPostCommand(`/${team.id()}/heartbeat`);
  return {};
});

HuntJS.get('/currentTime', async () => {
  return Number(await simGetValue('/currentTime'));
});

HuntJS.get('/levelData', ({ data }) => {
  if (!data || !data.level) {
    throw HuntJS.Error(422, 'No level given');
  }

  // this check should read from mysql
  // if (!((data.level === 1) || (data.level === 2))) {
  //   throw HuntJS.Error(422, 'You haven\'t unlocked that level');
  // }

  // TODO: actual level data
  return ['fake level data 1', 'fake level data 2'];
});

HuntJS.get('/teamStatus', async ({ team }) => {
  const teamData = await DB.fetchTeamData(team.id());
  console.log(teamData);

  const levelStatuses = teamData.levels.map((data, idx) => ({
    won: data.won,
    unlockedChunks: data.unlockedChunks,
    artifact: data.won ? artifactNames[idx] : null,
  }));

  return {
    level: teamData.level,
    difficulty: teamData.difficulty,
    deaths: teamData.deaths,
    levelStatuses,
  };
});

HuntJS.post('/changeLevel', async ({ data, team }) => {
  if (!data || !data.level) {
    throw HuntJS.Error(422, 'No level given');
  }

  if (!((data.level === 1) || (data.level === 2) || (data.level === 3))) {
    throw HuntJS.Error(422, `Invalid level: ${data.level}`);
  }

  if (data.level >= 2) {
    const teamData = await DB.fetchTeamData(team.id());
    if (!teamData || !teamData.levels[data.level - 2].won) {
      throw HuntJS.Error(422, 'You haven\'t unlocked that level');
    }
  }

  await simPostCommand(`/${team.id()}/changeLevel/${data.level}`);
  return 'fake level data';
});

HuntJS.onSubscribe('gameState', ({ team }) => {
  simPostCommand(`/${team.id()}/refreshGameState`);
});

HuntJS.post('/sendChatMessage', ({ data, team }) => {
  if (!data || !data.message || (typeof data.message !== 'string') || !data.name || (typeof data.name !== 'string')) {
    throw HuntJS.Error(422, 'Must provide non-empty strings for message and name parameters');
  }

  team.publish('chatMessages', JSON.stringify({
    message: data.message,
    name: data.name,
  }));

  return {};
}, {
  // team-wide rate limit to limit load on Redis
  rateLimitPerMinute: 300,
});


HuntJS.serve();
