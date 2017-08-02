#!/bin/sh -eo pipefail

if [[ -z "${DISCORD_BOT_TOKEN}" ]]; then
    echo 'Missing required env variable "DISCORD_BOT_TOKEN"'
    exit 1
fi

# If container is running, stop and remove it
container_id=$(docker ps --quiet --all --filter="name=owbot")
if [[ -n "${container_id}" ]]; then
    docker stop ${container_id}
    docker rm ${container_id}
fi

# Update docker image
docker pull verath/owbot-bot:latest

# Start the container
docker run \
        -d \
        --log-driver=gcplogs \
        --name=owbot \
        --restart=on-failure:10 \
        --volume=./db:/db \
        verath/owbot-bot:latest -token ${DISCORD_BOT_TOKEN}

# Remove old/no longer needed images
old_images=$(docker images --filter dangling=true -q 1>/dev/null)
if [[ -n "$old_images" ]]; then
    docker rmi ${old_images}
fi
