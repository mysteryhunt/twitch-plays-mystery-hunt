package main

import (
	"fmt"
	"hunt/tpmhsim/tpmhutils"
	"time"
)

// Commands from the API server
type moveCommand struct {
	teamID string
	dir    tpmhutils.Direction
}

type changeLevelCommand struct {
	teamID   string
	newLevel int8
}

// A set of teams. Used to track which teams we need to flush redis and/or
// mysql data for.
type teamSet map[string]bool

// Main simulation loop
func startSimulator(
	moveChannel chan *moveCommand,
	heartbeatChannel chan string,
	refreshGameStateChannel chan string,
	changeLevelChannel chan *changeLevelCommand,
) {
	teams := make(teamMap)

	redis, err := newRedisConnection(config.RedisURL, config.MysqlDB)
	if err != nil {
		log.Fatalf("Fatal: could not connect to Redis: %s", err)
	}

	mysql, err := newMysqlConnection(
		config.MysqlHost,
		config.MysqlPort,
		config.MysqlUser,
		config.MysqlPassword,
		config.MysqlDB)

	lastPrintedStatsAt := time.Now()
	frameDurations := []time.Duration{}

	maxFrameDuration := time.Second / time.Duration(config.MaxFPS)
	minSleepDuration := time.Millisecond * time.Duration(config.MinSleepMillis)

	for {
		frameStart := time.Now()

		redisNeedsFlush := make(teamSet)
		mysqlNeedsFlush := make(teamSet)

		// Process commands from the channels
		simulatorProcessMoves(moveChannel, teams, redisNeedsFlush, redis, mysqlNeedsFlush, mysql)
		simulatorProcessHeartbeats(heartbeatChannel, teams, redisNeedsFlush, redis, mysql)
		simulatorProcessRefreshRequests(refreshGameStateChannel, redisNeedsFlush)
		simulatorProcessLevelChanges(changeLevelChannel, teams, redisNeedsFlush, redis, mysqlNeedsFlush, mysql)

		// Simulate a frame
		// If there were changes:
		// - add to redisNeedsFlush
		// - add to mysqlNeedsFlush if any of
		//   - team has not won the level and the level is now won
		//   - (current chunk + 2) > unlockedChunks
		for teamID, team := range teams {
			simulatorRunFrame(teamID, team, redisNeedsFlush, redis, mysqlNeedsFlush, mysql)
		}

		// Flush redis and mysql
		for teamID := range redisNeedsFlush {
			levelStatus, err := teams.getLevelStatus(teamID)
			if err != nil {
				log.Errorf("Error fetching level status for team %s while flushing redis", teamID)
				continue
			}

			go redis.flushRedis(teamID, levelStatus.state.Serialize())
		}

		for teamID := range mysqlNeedsFlush {
			team := teams[teamID]
			if team == nil {
				log.Errorf("Error fetching level status for team %s while flushing mysql", teamID)
				continue
			}

			go mysql.flushMysql(teamID, team.toMysqlData())
		}

		// Prune abandoned games
		heartbeatCutoff := time.Now().Add(-1 * time.Duration(config.MinHeartbeatInterval) * time.Second)
		numEvicted := 0
		for teamID, team := range teams {
			if team.lastHeartbeat.Before(heartbeatCutoff) {
				delete(teams, teamID)
				numEvicted++
			}
		}

		if numEvicted > 0 {
			log.Infof("Evicted %d teams", numEvicted)
		}

		// Log status
		frameEnd := time.Now()
		frameDuration := frameEnd.Sub(frameStart)
		frameDurations = append(frameDurations, frameDuration)

		if frameEnd.After(lastPrintedStatsAt.Add(time.Second)) {
			// It's been a second since we printed stats
			timeElapsed := frameEnd.Sub(lastPrintedStatsAt)
			frameCount := len(frameDurations)
			averageFrameDuration := averageDuration(frameDurations)

			actualFPS := float64(frameCount) / timeElapsed.Seconds()
			theoreticalMaxFPS := 1 / averageFrameDuration.Seconds()

			log.Infow(fmt.Sprintf("Running at %.2f FPS", actualFPS),
				"timeElapsed", timeElapsed,
				"frameCount", frameCount,
				"averageFrameDuration", averageFrameDuration,
				"actualFPS", actualFPS,
				"theoreticalMaxFPS", theoreticalMaxFPS,
				"numTeams", len(teams))

			// reset duration history
			frameDurations = []time.Duration{}

			lastPrintedStatsAt = frameEnd
		}

		// Sleep
		sleepTime := maxFrameDuration - frameDuration
		if sleepTime < minSleepDuration {
			time.Sleep(minSleepDuration)
		} else {
			time.Sleep(sleepTime)
		}
	}
}

// Channel processors
func simulatorProcessMoves(moveChannel chan *moveCommand, teams teamMap, redisNeedsFlush teamSet, redis *redisConnection, mysqlNeedsFlush teamSet, mysql *mysqlConnection) {
	for i := 0; i < config.MaxBufferLength; i++ {
		select {
		case moveCmd := <-moveChannel:
			teamID := moveCmd.teamID

			didLoad, err := teams.touch(teamID, redis, mysql)
			if err != nil {
				log.Errorw("Encountered an error touching team state while processing move command",
					"error", err,
					"teamID", teamID)
				continue
			}

			if didLoad {
				redisNeedsFlush[teamID] = true
			}

			levelStatus, err := teams.getLevelStatus(teamID)
			if err != nil {
				log.Errorw("Encountered an error getting level state while processing move command",
					"error", err,
					"teamID", teamID)
				continue
			}

			deltaX, deltaY := moveCmd.dir.Offsets()

			if levelStatus.state.MoveNinja(deltaX, deltaY) {
				redisNeedsFlush[teamID] = true

				team := teams[teamID]
				if team == nil {
					log.Errorf("Error fetching level status for team %s while preparing for post-move simulation", teamID)
					continue
				}

				simulatorRunFrame(teamID, team, redisNeedsFlush, redis, mysqlNeedsFlush, mysql)
			}
		default:
			return
		}
	}
}

func simulatorProcessHeartbeats(heartbeatChannel chan string, teams teamMap, redisNeedsFlush teamSet, redis *redisConnection, mysql *mysqlConnection) {
	for i := 0; i < config.MaxBufferLength; i++ {
		select {
		case teamID := <-heartbeatChannel:
			didLoad, err := teams.touch(teamID, redis, mysql)
			if err != nil {
				log.Errorw("Encountered an error touching team state while processing heartbeat command",
					"error", err,
					"teamID", teamID)
			}

			if didLoad {
				redisNeedsFlush[teamID] = true
			}
		default:
			return
		}
	}
}

func simulatorProcessRefreshRequests(refreshGameStateChannel chan string, redisNeedsFlush teamSet) {
	for i := 0; i < config.MaxBufferLength; i++ {
		select {
		case teamID := <-refreshGameStateChannel:
			redisNeedsFlush[teamID] = true
		default:
			return
		}
	}
}

func simulatorProcessLevelChanges(
	changeLevelChannel chan *changeLevelCommand,
	teams teamMap,
	redisNeedsFlush teamSet,
	redis *redisConnection,
	mysqlNeedsFlush teamSet,
	mysql *mysqlConnection,
) {
	for i := 0; i < config.MaxBufferLength; i++ {
		select {
		case changeLevelCommand := <-changeLevelChannel:
			teamID := changeLevelCommand.teamID
			newLevel := changeLevelCommand.newLevel

			_, err := teams.touch(teamID, redis, mysql)
			if err != nil {
				log.Errorw("Encountered an error touching team state while processing changeLevel command",
					"error", err,
					"teamID", teamID)
				continue
			}

			team := teams[teamID]
			if team == nil {
				log.Errorw("Team was nil while processing changeLevel command",
					"teamID", teamID)
				continue
			}

			if (newLevel < 1) || (newLevel > 3) {
				log.Errorw("changeLevel command issues for invalid level",
					"teamID", teamID,
					"level", newLevel)
			}

			team.currentLevel = newLevel
			team.resetLevelStates()

			redisNeedsFlush[teamID] = true
			mysqlNeedsFlush[teamID] = true

		default:
			return
		}
	}
}

func simulatorRunFrame(
	teamID string,
	team *teamData,
	redisNeedsFlush teamSet,
	redis *redisConnection,
	mysqlNeedsFlush teamSet,
	mysql *mysqlConnection,
) {
	teamLevelStatus := team.getLevelStatus()
	teamLevelState := teamLevelStatus.state

	didChange, messages, didDie := teamLevelState.RunFrame(team.godModeActive(), team.difficulty)

	if didChange {
		redisNeedsFlush[teamID] = true

		if (teamLevelState.CurrentChunk() + 2) > teamLevelStatus.unlockedChunks {
			teamLevelStatus.unlockedChunks = (teamLevelState.CurrentChunk() + 2)
			mysqlNeedsFlush[teamID] = true
		}
	}

	if (messages != nil) && (len(messages) > 0) {
		for _, message := range messages {
			go redis.publishMessage(teamID, message)
		}
	}

	if didDie {
		go mysql.incrementDeathCounter(teamID, redis)
	}

	if teamLevelState.IsWon() {
		msg := fmt.Sprintf("You have found the %s!", teamLevelState.ArtifactName())

		if team.currentLevel == 3 {
			msg += " You've found all three legendary treasures!"
		}

		go redis.publishMessage(teamID, msg)

		if team.currentLevel == 3 {
			go redis.publishMessage(teamID, "You may have <b>evaded my guards</b>, <b>traversed my dungeons</b>, and <b>stolen my treasures</b>, but you will still need the password in order to confront me in another puzzle!")
			go redis.publishMessage(teamID, "Your ninja ability has leveled up! You can now hide in plain sight, walk on lava, and you are invulnerable to all forms of damage.")
		}

		teamLevelStatus.won = true
		team.currentLevel = team.currentLevel + 1
		if team.currentLevel > 3 {
			team.currentLevel = 1
		}

		team.resetLevelStates()

		redisNeedsFlush[teamID] = true
		mysqlNeedsFlush[teamID] = true
	}
}
