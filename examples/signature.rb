require "openssl"
require "base64"

public_key = [ENV['IMGPROXY_PUBLIC_KEY']].pack("H*")
puts "public_key=#{public_key}"

secret_key = [ENV['IMGPROXY_SECRET_KEY']].pack("H*")
puts "secret_key=#{secret_key}"

salt = [ENV['IMGPROXY_SALT']].pack("H*")
puts "salt=#{salt}"


base_url = ARGV[0] # https://domain.com/tile/client_id
puts "base_url=#{base_url}"

tile_path = ARGV[1] # /0/0/0.png
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
