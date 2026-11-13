#!/bin/sh
# Adds the .deb files from <deb folder> to the APT repository in <site
# folder> (the gh-pages branch) and signs it with the key in gpg:
# apt-repo.sh <site folder> <deb folder>
set -eu
site=$1 debs=$2
pool="$site/pool/main/t/ttrace"
mkdir -p "$pool"
cp "$debs"/*.deb "$pool/"
cd "$site"
dist=dists/stable
for arch in amd64 arm64; do
	mkdir -p "$dist/main/binary-$arch"
	apt-ftparchive --arch "$arch" packages pool > "$dist/main/binary-$arch/Packages"
	gzip -9nkf "$dist/main/binary-$arch/Packages"
done
rm -f "$dist/Release" "$dist/InRelease" "$dist/Release.gpg"
apt-ftparchive \
	-o APT::FTPArchive::Release::Origin=ttrace \
	-o APT::FTPArchive::Release::Label=ttrace \
	-o APT::FTPArchive::Release::Suite=stable \
	-o APT::FTPArchive::Release::Codename=stable \
	-o APT::FTPArchive::Release::Architectures="amd64 arm64" \
	-o APT::FTPArchive::Release::Components=main \
	-o APT::FTPArchive::Release::Description="ttrace packages" \
	release "$dist" > /tmp/Release
mv /tmp/Release "$dist/Release"
gpg --batch --yes --clearsign -o "$dist/InRelease" "$dist/Release"
gpg --batch --yes --armor --detach-sign -o "$dist/Release.gpg" "$dist/Release"
touch .nojekyll
