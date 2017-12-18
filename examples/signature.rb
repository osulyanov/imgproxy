require "openssl"
require "base64"

puts "public_key=#{ENV['IMGPROXY_PUBLIC_KEY']}"
puts "secret_key=#{ENV['IMGPROXY_SECRET_KEY']}"
puts "salt=#{ENV['IMGPROXY_SALT']}"

public_key = [ENV['IMGPROXY_PUBLIC_KEY']].pack("H*")
secret_key = [ENV['IMGPROXY_SECRET_KEY']].pack("H*")
salt = [ENV['IMGPROXY_SALT']].pack("H*")

base_url = ARGV[0] # http://mapwarper.net/maps/tile/26645/
puts "base_url=#{base_url}"

tile_path = ARGV[1] # 4/0/0/0.png
puts "tile_path=#{tile_path}"

digest = OpenSSL::Digest.new("sha256")
signed_base_url = Base64.urlsafe_encode64(OpenSSL::HMAC.digest(digest, secret_key, "#{salt}#{base_url}")).tr("=", "")
puts "signed_base_url=#{signed_base_url}"


url = "#{signed_base_url}#{tile_path}"
puts "url=#{url}"


encoded_url = Base64.urlsafe_encode64(url).tr("=", "").scan(/.{1,16}/).join("/")
puts "encoded_url=#{encoded_url}"


extension = "png"

path = "/---/#{encoded_url}.#{extension}"
puts "path=#{path}"


hmac = Base64.urlsafe_encode64(OpenSSL::HMAC.digest(digest, public_key, "#{salt}#{path}")).tr("=", "")

puts "final full path:"
puts "/#{hmac}#{path}"
