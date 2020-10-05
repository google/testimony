#!/usr/bin/env bash

if [ ! -f /usr/sbin/testimonyd ]; then
	sudo cp -v go/testimonyd/testimonyd /usr/sbin/testimonyd
fi

if [ ! -f /etc/testimony.conf ]; then
	sudo cp -v configs/testimony.conf /etc/testimony.conf
fi

if [ ! -f /etc/systemd/system/testimony.service ]; then
	sudo cp -v configs/systemd.conf /etc/systemd/system/testimony.service
	sudo chmod 0644 /etc/systemd/system/testimony.service
fi

sudo service testimony start